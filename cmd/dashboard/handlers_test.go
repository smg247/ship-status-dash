package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ship-status-dash/pkg/auth"
	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/repositories"
	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

// newTestHandlers returns Handlers backed by cfg, the given outage manager, mock pings, and a mock group cache.
func newTestHandlers(t *testing.T, cfg *types.DashboardConfig, om outage.OutageManager) *Handlers {
	return newTestHandlersWithGroups(t, cfg, om, nil)
}

// newTestHandlersWithGroups is like newTestHandlers but pre-populates group membership.
func newTestHandlersWithGroups(t *testing.T, cfg *types.DashboardConfig, om outage.OutageManager, groups map[string][]string) *Handlers {
	return newTestHandlersWithSLO(t, cfg, om, groups, &repositories.MockSLOWorkspaceRepository{})
}

func newTestHandlersWithSLO(t *testing.T, cfg *types.DashboardConfig, om outage.OutageManager, groups map[string][]string, sloRepo repositories.SLOWorkspaceRepository) *Handlers {
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

func TestIsUserAuthorizedForTeamSLO(t *testing.T) {
	component := &types.Component{
		Name: "TRT Incidents", Slug: "trt-incidents", SLOComponent: true,
		Owners: []types.Owner{{User: "component-owner"}, {User: "both"}},
	}
	cfg := &types.DashboardConfig{
		Components: []*types.Component{component},
		TeamSLOs: []types.TeamSLOConfig{{
			Team: "TRT",
			Owners: []types.Owner{
				{User: "team-user"},
				{User: "both"},
				{ServiceAccount: "system:serviceaccount:ship-status:chai-bot"},
				{RoverGroup: "trt-group"},
			},
			SLOComponents: []string{"trt-incidents"},
		}},
	}
	groups := map[string][]string{"trt-group": {"group-member"}}
	h := newTestHandlersWithGroups(t, cfg, &outage.MockOutageManager{}, groups)

	tests := []struct {
		name       string
		user       string
		team       string
		authorized bool
	}{
		{name: "team user", user: "team-user", team: "TRT", authorized: true},
		{name: "team service account", user: "system:serviceaccount:ship-status:chai-bot", team: "TRT", authorized: true},
		{name: "team rover group member", user: "group-member", team: "TRT", authorized: true},
		{name: "component owner is not a team SLO owner", user: "component-owner", team: "TRT", authorized: false},
		{name: "unknown team", user: "team-user", team: "Nope", authorized: false},
		{name: "unknown user", user: "stranger", team: "TRT", authorized: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.authorized, h.IsUserAuthorizedForTeamSLO(tt.user, tt.team))
		})
	}

	userTests := []struct {
		name       string
		user       string
		components []string
		teams      []string
	}{
		{name: "component owner", user: "component-owner", components: []string{"trt-incidents"}, teams: []string{}},
		{name: "team owner", user: "team-user", components: []string{}, teams: []string{"TRT"}},
		{name: "both", user: "both", components: []string{"trt-incidents"}, teams: []string{"TRT"}},
		{name: "rover group member", user: "group-member", components: []string{}, teams: []string{"TRT"}},
	}
	for _, tt := range userTests {
		t.Run("api user "+tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/user", nil)
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, tt.user))
			rr := httptest.NewRecorder()
			h.GetAuthenticatedUserJSON(rr, req)
			require.Equal(t, http.StatusOK, rr.Code)
			var got AuthenticatedUser
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			assert.Equal(t, tt.components, got.Components)
			assert.Equal(t, tt.teams, got.TeamSLOs)
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

func sloHandlerConfig() *types.DashboardConfig {
	raw := []byte(`{"window":"24h","min_accepted":1,"recent_payloads":2,"streams":[{"release_controller":"amd64","name":"nightly"}]}`)
	return &types.DashboardConfig{
		Components: []*types.Component{{
			Name: "TRT Incidents", Slug: "trt-incidents", SLOComponent: true,
			Subcomponents: []types.SubComponent{{Name: "Incidents", Slug: "incidents"}},
		}},
		TeamSLOs: []types.TeamSLOConfig{{
			Team:          "TRT",
			Owners:        []types.Owner{{User: "developer"}},
			SLOComponents: []string{"trt-incidents"},
			SLOs: []types.NamedSLO{{
				Name:   "accepted-payload-per-day",
				Source: payloadv1.Source,
				Workspace: &types.SLOWorkspace{
					Kind:          payloadv1.Kind,
					SchemaVersion: payloadv1.SchemaVersion,
					Spec:          raw,
				},
			}},
		}},
	}
}

const validSLOItemBody = `{
	"kind":"payload_streams",
	"schema_version":1,
	"item_key":"new-item",
	"group_key":"nightly",
	"occurred_at":"2026-09-25T12:00:00Z",
	"outcome":"Rejected",
	"details":{"payload_url":"https://example.com/p","jobs":[{"name":"job","url":"https://example.com/j","state":"failure"}]}
}`

func keptSLOItem() types.SLOWorkspaceItem {
	return types.SLOWorkspaceItem{
		Model:    gorm.Model{ID: 3},
		Team:     "TRT",
		Kind:     payloadv1.Kind,
		ItemKey:  "keep",
		GroupKey: "nightly",
		Outcome:  "Rejected",
		Links: []types.SLOWorkspaceLink{{
			Model:    gorm.Model{ID: 9},
			ItemID:   3,
			URL:      "https://example.com/l",
			LinkType: "jira",
		}},
	}
}

func TestGetTeamSLOJSON(t *testing.T) {
	tests := []struct {
		name      string
		listErr   error
		outageErr bool
		wantCode  int
		wantErr   string
	}{
		{
			name:     "list failure",
			listErr:  errors.New("db"),
			wantCode: http.StatusInternalServerError,
			wantErr:  "Failed to load team SLO",
		},
		{
			name:      "outage lookup failure",
			outageErr: true,
			wantCode:  http.StatusInternalServerError,
			wantErr:   "Failed to load team SLO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &repositories.MockSLOWorkspaceRepository{ListErr: tt.listErr}
			om := &outage.MockOutageManager{}
			if tt.outageErr {
				om.GetActiveOutagesForComponentFn = func(string) ([]types.Outage, error) {
					return nil, errors.New("lookup")
				}
			}
			h := newTestHandlersWithSLO(t, sloHandlerConfig(), om, nil, repo)

			req := httptest.NewRequest(http.MethodGet, "/api/teams/TRT/slo", nil)
			req = mux.SetURLVars(req, map[string]string{"team": "TRT"})
			rr := httptest.NewRecorder()
			h.GetTeamSLOJSON(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			var got map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			assert.Equal(t, tt.wantErr, got["error"])
		})
	}
}

func TestGetTeamSLOSummaryJSON(t *testing.T) {
	repo := &repositories.MockSLOWorkspaceRepository{ListErr: errors.New("db")}
	h := newTestHandlersWithSLO(t, sloHandlerConfig(), &outage.MockOutageManager{}, nil, repo)

	req := httptest.NewRequest(http.MethodGet, "/api/teams/slo-summary", nil)
	rr := httptest.NewRecorder()
	h.GetTeamSLOSummaryJSON(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	var got map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "Failed to load SLO summary", got["error"])
}

func TestPutSLOItemJSON(t *testing.T) {
	item := keptSLOItem()
	repo := &repositories.MockSLOWorkspaceRepository{
		Items:    []types.SLOWorkspaceItem{item},
		WriteErr: errors.New("db"),
	}
	h := newTestHandlersWithSLO(t, sloHandlerConfig(), &outage.MockOutageManager{}, nil, repo)

	req := httptest.NewRequest(http.MethodPut, "/api/teams/TRT/slo/items", bytes.NewBufferString(validSLOItemBody))
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, "developer"))
	req = mux.SetURLVars(req, map[string]string{"team": "TRT"})
	rr := httptest.NewRecorder()
	h.PutSLOItemJSON(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
	var got map[string]string
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	assert.Equal(t, "Failed to save SLO item", got["error"])
	assert.Equal(t, []types.SLOWorkspaceItem{item}, repo.Items)
}

func TestDeleteSLOItemJSON(t *testing.T) {
	tests := []struct {
		name     string
		itemKey  string
		writeErr error
		wantCode int
		wantErr  string
	}{
		{
			name:     "missing item",
			itemKey:  "missing",
			wantCode: http.StatusNotFound,
			wantErr:  "SLO item not found",
		},
		{
			name:     "write failure",
			itemKey:  "keep",
			writeErr: errors.New("db"),
			wantCode: http.StatusInternalServerError,
			wantErr:  "Failed to delete SLO item",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := keptSLOItem()
			repo := &repositories.MockSLOWorkspaceRepository{
				Items:    []types.SLOWorkspaceItem{item},
				WriteErr: tt.writeErr,
			}
			h := newTestHandlersWithSLO(t, sloHandlerConfig(), &outage.MockOutageManager{}, nil, repo)

			req := httptest.NewRequest(http.MethodDelete, "/api/teams/TRT/slo/items/"+payloadv1.Kind+"/"+tt.itemKey, nil)
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, "developer"))
			req = mux.SetURLVars(req, map[string]string{
				"team": "TRT", "kind": payloadv1.Kind, "itemKey": tt.itemKey,
			})
			rr := httptest.NewRecorder()
			h.DeleteSLOItemJSON(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			var got map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			assert.Equal(t, tt.wantErr, got["error"])
			assert.Equal(t, []types.SLOWorkspaceItem{item}, repo.Items)
		})
	}
}

func TestPutSLOItemLinkJSON(t *testing.T) {
	tests := []struct {
		name     string
		itemKey  string
		body     string
		listErr  error
		writeErr error
		wantCode int
		wantErr  string
	}{
		{
			name:     "write failure",
			itemKey:  "keep",
			body:     `{"url":"https://example.com/new","link_type":"jira"}`,
			writeErr: errors.New("db"),
			wantCode: http.StatusInternalServerError,
			wantErr:  "Failed to add SLO link",
		},
		{
			name:     "list failure",
			itemKey:  "keep",
			body:     `{"url":"https://example.com/new","link_type":"jira"}`,
			listErr:  errors.New("db"),
			wantCode: http.StatusInternalServerError,
			wantErr:  "Failed to load SLO item",
		},
		{
			name:     "empty url",
			itemKey:  "keep",
			body:     `{"url":" ","link_type":"jira"}`,
			wantCode: http.StatusBadRequest,
			wantErr:  "url is required",
		},
		{
			name:     "missing item",
			itemKey:  "missing",
			body:     `{"url":"https://example.com/new","link_type":"jira"}`,
			wantCode: http.StatusNotFound,
			wantErr:  "SLO item not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := keptSLOItem()
			repo := &repositories.MockSLOWorkspaceRepository{
				Items:    []types.SLOWorkspaceItem{item},
				ListErr:  tt.listErr,
				WriteErr: tt.writeErr,
			}
			h := newTestHandlersWithSLO(t, sloHandlerConfig(), &outage.MockOutageManager{}, nil, repo)

			req := httptest.NewRequest(http.MethodPut, "/api/teams/TRT/slo/items/"+payloadv1.Kind+"/"+tt.itemKey+"/links", bytes.NewBufferString(tt.body))
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, "developer"))
			req = mux.SetURLVars(req, map[string]string{
				"team": "TRT", "kind": payloadv1.Kind, "itemKey": tt.itemKey,
			})
			rr := httptest.NewRecorder()
			h.PutSLOItemLinkJSON(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			var got map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			assert.Equal(t, tt.wantErr, got["error"])
			assert.Equal(t, []types.SLOWorkspaceItem{item}, repo.Items)
		})
	}
}

func TestDeleteSLOItemLinkJSON(t *testing.T) {
	tests := []struct {
		name     string
		linkID   string
		writeErr error
		wantCode int
		wantErr  string
	}{
		{
			name:     "missing link",
			linkID:   "4",
			wantCode: http.StatusNotFound,
			wantErr:  "SLO link not found",
		},
		{
			name:     "write failure",
			linkID:   "9",
			writeErr: errors.New("db"),
			wantCode: http.StatusInternalServerError,
			wantErr:  "Failed to delete SLO link",
		},
		{
			name:     "bad link id",
			linkID:   "nope",
			wantCode: http.StatusBadRequest,
			wantErr:  "invalid link id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := keptSLOItem()
			repo := &repositories.MockSLOWorkspaceRepository{
				Items:    []types.SLOWorkspaceItem{item},
				WriteErr: tt.writeErr,
			}
			h := newTestHandlersWithSLO(t, sloHandlerConfig(), &outage.MockOutageManager{}, nil, repo)

			req := httptest.NewRequest(http.MethodDelete, "/api/teams/TRT/slo/items/"+payloadv1.Kind+"/keep/links/"+tt.linkID, nil)
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, "developer"))
			req = mux.SetURLVars(req, map[string]string{
				"team": "TRT", "kind": payloadv1.Kind, "itemKey": "keep", "linkId": tt.linkID,
			})
			rr := httptest.NewRecorder()
			h.DeleteSLOItemLinkJSON(rr, req)

			assert.Equal(t, tt.wantCode, rr.Code)
			var got map[string]string
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
			assert.Equal(t, tt.wantErr, got["error"])
			assert.Equal(t, []types.SLOWorkspaceItem{item}, repo.Items)
		})
	}
}
