package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ship-status-dash/pkg/auth"
	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/types"
)

// newTestHandlers returns Handlers backed by cfg, the given outage manager, mock pings, and a mock group cache.
func newTestHandlers(t *testing.T, cfg *types.DashboardConfig, om outage.OutageManager) *Handlers {
	return newTestHandlersWithGroups(t, cfg, om, nil)
}

// newTestHandlersWithGroups is like newTestHandlers but pre-populates group membership.
func newTestHandlersWithGroups(t *testing.T, cfg *types.DashboardConfig, om outage.OutageManager, groups map[string][]string) *Handlers {
	t.Helper()
	cfgManager, err := config.NewManager("", func(string) (*types.DashboardConfig, error) {
		return cfg, nil
	}, logrus.New(), time.Second)
	require.NoError(t, err)
	cfgManager.Get()

	pingRepo := &repositories.MockComponentPingRepository{}
	triageNoteRepo := &repositories.MockTriageNoteRepository{}
	outageLinkRepo := &repositories.MockOutageLinkRepository{}
	cache := &auth.MockGroupMembershipProvider{Groups: groups}
	sloRepo := &repositories.MockSLOWorkspaceRepository{}
	return NewHandlers(logrus.New(), cfgManager, om, pingRepo, triageNoteRepo, outageLinkRepo, sloRepo, cache)
}

// minimalDashboardConfig is a tiny valid config (one component, one sub-component) for handler tests.
func minimalDashboardConfig() *types.DashboardConfig {
	return &types.DashboardConfig{
		Components: []*types.Component{
			{
				Name: "Alpha", Slug: "alpha", ShipTeam: "team-a",
				Subcomponents: []types.SubComponent{
					{Name: "One", Slug: "one"},
				},
			},
		},
	}
}

func TestIsUserAuthorizedForComponent(t *testing.T) {
	component := &types.Component{
		Name: "Test", Slug: "test",
		Owners: []types.Owner{
			{User: "developer"},
			{RoverGroup: "test-group"},
			{ServiceAccount: "system:serviceaccount:ship-status:chai-bot"},
		},
	}

	tests := []struct {
		name       string
		user       string
		authorized bool
	}{
		{
			name:       "user owner is authorized",
			user:       "developer",
			authorized: true,
		},
		{
			name:       "service account owner is authorized via Owner.ServiceAccount",
			user:       "system:serviceaccount:ship-status:chai-bot",
			authorized: true,
		},
		{
			name:       "rover group member is authorized",
			user:       "groupuser",
			authorized: true,
		},
		{
			name:       "unlisted user is not authorized",
			user:       "stranger",
			authorized: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &types.DashboardConfig{Components: []*types.Component{component}}
			groups := map[string][]string{"test-group": {"groupuser", "anotheruser"}}
			h := newTestHandlersWithGroups(t, cfg, &outage.MockOutageManager{}, groups)
			assert.Equal(t, tt.authorized, h.IsUserAuthorizedForComponent(tt.user, component))
		})
	}
}

func TestGetComponentStatusJSON_CriticalSubComponent(t *testing.T) {
	now := time.Now()
	cfg := &types.DashboardConfig{
		Components: []*types.Component{
			{
				Name: "Alpha", Slug: "alpha",
				Subcomponents: []types.SubComponent{
					{Name: "Critical One", Slug: "critical-one", Critical: true},
					{Name: "Critical Three", Slug: "critical-three", Critical: true},
					{Name: "Normal Two", Slug: "normal-two"},
				},
			},
		},
	}

	confirmedOutage := func(sub string, sev types.Severity) types.Outage {
		return types.Outage{
			ComponentName:    "alpha",
			SubComponentName: sub,
			Severity:         sev,
			ConfirmedAt:      sql.NullTime{Time: now, Valid: true},
		}
	}

	tests := []struct {
		name             string
		outages          []types.Outage
		suspectedOutages []types.Outage
		expectedStatus   types.Status
	}{
		{
			name:           "critical sub-component down bypasses Partial",
			outages:        []types.Outage{confirmedOutage("critical-one", types.SeverityDown)},
			expectedStatus: types.StatusDown,
		},
		{
			name:           "critical sub-component degraded bypasses Partial",
			outages:        []types.Outage{confirmedOutage("critical-one", types.SeverityDegraded)},
			expectedStatus: types.StatusDegraded,
		},
		{
			name: "suspected outage on critical sub-component shows Suspected",
			suspectedOutages: []types.Outage{
				{ComponentName: "alpha", SubComponentName: "critical-one", Severity: types.SeveritySuspected},
			},
			expectedStatus: types.StatusSuspected,
		},
		{
			name: "multiple critical sub-components: most severe wins",
			outages: []types.Outage{
				confirmedOutage("critical-one", types.SeverityDown),
				confirmedOutage("critical-three", types.SeverityDegraded),
			},
			expectedStatus: types.StatusDown,
		},
		{
			name: "all sub-components affected uses most severe status",
			outages: []types.Outage{
				confirmedOutage("critical-one", types.SeverityDegraded),
				confirmedOutage("normal-two", types.SeverityDown),
				confirmedOutage("critical-three", types.SeverityDegraded),
			},
			expectedStatus: types.StatusDown,
		},
		{
			name:           "non-critical sub-component only shows Partial",
			outages:        []types.Outage{confirmedOutage("normal-two", types.SeverityDown)},
			expectedStatus: types.StatusPartial,
		},
		{
			name:           "no outages shows Healthy",
			outages:        []types.Outage{},
			expectedStatus: types.StatusHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOM := &outage.MockOutageManager{}
			mockOM.GetActiveOutagesForComponentFn = func(slug string) ([]types.Outage, error) {
				return tt.outages, nil
			}
			mockOM.GetActiveSuspectedOutagesForComponentFn = func(slug string) ([]types.Outage, error) {
				return tt.suspectedOutages, nil
			}

			h := newTestHandlers(t, cfg, mockOM)
			got, err := h.getComponentStatus(cfg.Components[0], logrus.NewEntry(logrus.New()))
			require.NoError(t, err)
			assert.Equal(t, tt.expectedStatus, got.Status)
		})
	}
}

func TestGetOutagesDuringJSON(t *testing.T) {
	cfg := minimalDashboardConfig()
	t0 := time.Date(2025, 4, 1, 10, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Hour)
	t2 := t0.Add(2 * time.Hour)

	mockOM := &outage.MockOutageManager{}
	mockOM.GetOutagesDuringFn = func(queryStart, queryEnd time.Time, refs []types.SubComponentRef) ([]types.Outage, error) {
		if len(refs) == 0 {
			return []types.Outage{}, nil
		}
		if len(refs) == 1 && refs[0].ComponentSlug == "alpha" && refs[0].SubSlug == "one" &&
			queryStart.Equal(t1) && queryEnd.Equal(t1) {
			return []types.Outage{{
				ComponentName:    "alpha",
				SubComponentName: "one",
				Severity:         types.SeverityDown,
				StartTime:        t0,
				Description:      "x",
				DiscoveredFrom:   "test",
				CreatedBy:        "u",
			}}, nil
		}
		return []types.Outage{}, nil
	}

	h := newTestHandlers(t, cfg, mockOM)

	intPtr := func(n int) *int { return &n }

	tests := []struct {
		name            string
		query           string
		wantCode        int
		wantOutageCount *int
	}{
		{
			name:            "200_with_start_only",
			query:           "start=" + t1.UTC().Format(time.RFC3339),
			wantCode:        http.StatusOK,
			wantOutageCount: intPtr(1),
		},
		{
			name:     "400_no_time_params",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "400_sub_without_component",
			query:    "start=" + t0.Format(time.RFC3339) + "&subComponentName=one",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "400_start_after_end",
			query:    "start=" + t2.Format(time.RFC3339) + "&end=" + t0.Format(time.RFC3339),
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "404_unknown_component",
			query:    "start=" + t0.Format(time.RFC3339) + "&componentName=nope",
			wantCode: http.StatusNotFound,
		},
		{
			name:     "404_unknown_sub",
			query:    "start=" + t0.Format(time.RFC3339) + "&componentName=alpha&subComponentName=nope",
			wantCode: http.StatusNotFound,
		},
		{
			name:            "200_empty_when_tag_excludes",
			query:           "start=" + t1.Format(time.RFC3339) + "&tag=nonexistent-tag",
			wantCode:        http.StatusOK,
			wantOutageCount: intPtr(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := "/api/outages/during"
			if tt.query != "" {
				path += "?" + tt.query
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.GetOutagesDuringJSON(rec, req)
			res := rec.Result()
			defer res.Body.Close()

			assert.Equal(t, tt.wantCode, res.StatusCode)
			if tt.wantOutageCount == nil {
				return
			}
			var got []types.Outage
			require.NoError(t, json.NewDecoder(res.Body).Decode(&got))
			assert.Len(t, got, *tt.wantOutageCount)
		})
	}
}

func TestParseStatusFilters(t *testing.T) {
	tests := []struct {
		name    string
		raw     []string
		want    []types.Status
		wantErr string
	}{
		{name: "empty", raw: nil, want: nil},
		{name: "single", raw: []string{"Down"}, want: []types.Status{types.StatusDown}},
		{
			name: "repeated",
			raw:  []string{"Down", "Degraded"},
			want: []types.Status{types.StatusDown, types.StatusDegraded},
		},
		{
			name: "comma-separated",
			raw:  []string{"Down,Degraded"},
			want: []types.Status{types.StatusDown, types.StatusDegraded},
		},
		{
			name: "mixed with spaces and duplicates",
			raw:  []string{"Down, Degraded", "Down", "Suspected"},
			want: []types.Status{types.StatusDown, types.StatusDegraded, types.StatusSuspected},
		},
		{name: "invalid", raw: []string{"Nope"}, wantErr: "invalid status: Nope"},
		{name: "partial rejected", raw: []string{"Partial"}, wantErr: "invalid status: Partial"},
		{name: "blank only", raw: []string{", ,"}, wantErr: "status filter must include at least one status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, errMsg := parseStatusFilters(tt.raw)
			if tt.wantErr != "" {
				assert.Equal(t, tt.wantErr, errMsg)
				assert.Nil(t, got)
				return
			}
			assert.Empty(t, errMsg)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestListAPIsOmitSLOComponent(t *testing.T) {
	cfg := &types.DashboardConfig{
		Components: []*types.Component{
			{
				Name: "Sippy", Slug: "sippy", ShipTeam: "TRT",
				Subcomponents: []types.SubComponent{{Name: "Sippy", Slug: "sippy"}},
			},
			{
				Name: "TRT Incidents", Slug: "trt-incidents", ShipTeam: "TRT", SLOComponent: true,
				Subcomponents: []types.SubComponent{{Name: "Incidents", Slug: "incidents"}},
			},
		},
	}
	h := newTestHandlers(t, cfg, &outage.MockOutageManager{})

	rr := httptest.NewRecorder()
	h.GetComponentsJSON(rr, httptest.NewRequest(http.MethodGet, "/api/components", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var components []types.Component
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &components))
	require.Len(t, components, 1)
	assert.Equal(t, "Sippy", components[0].Name)

	rr = httptest.NewRecorder()
	h.ListSubComponentsJSON(rr, httptest.NewRequest(http.MethodGet, "/api/sub-components?team=TRT", nil))
	require.Equal(t, http.StatusOK, rr.Code)
	var subs []types.SubComponentListItem
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &subs))
	require.Len(t, subs, 1)
	assert.Equal(t, "Sippy", subs[0].ComponentName)
}
