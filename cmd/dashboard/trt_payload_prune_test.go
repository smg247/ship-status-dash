package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
)

func TestGetTeamSLODoesNotDeleteExpiredItems(t *testing.T) {
	now := time.Now().UTC()
	repo := &repositories.MockSLOWorkspaceRepository{
		Items: []types.SLOWorkspaceItem{
			{Model: gorm.Model{ID: 1}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "old-rejected", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
			{Model: gorm.Model{ID: 2}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "recent", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
			{Model: gorm.Model{ID: 3}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "newer", GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-time.Hour)},
		},
	}
	h := newSLOHandlers(t, sloTeamConfig(2), repo)

	req := httptest.NewRequest(http.MethodGet, "/api/teams/TRT/slo", nil)
	req = mux.SetURLVars(req, map[string]string{"team": "TRT"})
	rr := httptest.NewRecorder()
	h.GetTeamSLOJSON(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	var view slo.TeamView
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &view))
	assert.NotContains(t, view.ItemKeys, "old-rejected")
	assert.ElementsMatch(t, []string{"recent", "newer"}, view.ItemKeys)
	require.Len(t, repo.Items, 3)
}

func TestTRTPayloadPrunerDeletesOnlyExpiredRows(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	repo := &repositories.MockSLOWorkspaceRepository{
		Items: []types.SLOWorkspaceItem{
			{Model: gorm.Model{ID: 1}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "old-rejected", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
			{Model: gorm.Model{ID: 2}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "recent", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
			{Model: gorm.Model{ID: 3}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "newer", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-time.Hour)},
			{Model: gorm.Model{ID: 4}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "kept-accepted", GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
		},
	}
	pruner := NewTRTPayloadPruner(sloConfigManager(t, sloTeamConfig(2)), repo, time.Minute, logrus.New())
	pruner.prune()

	var keys []string
	for _, item := range repo.Items {
		keys = append(keys, item.ItemKey)
	}
	assert.ElementsMatch(t, []string{"recent", "newer", "kept-accepted"}, keys)
}

func TestTRTPayloadPrunerKeepsRowUpdatedBeforeDelete(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	repo := &repositories.MockSLOWorkspaceRepository{
		Items: []types.SLOWorkspaceItem{
			{Model: gorm.Model{ID: 1}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "was-expired", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
			{Model: gorm.Model{ID: 2}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "recent", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
			{Model: gorm.Model{ID: 3}, Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "newer", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-time.Hour)},
		},
	}
	_, err := repo.UpsertItem(&types.SLOWorkspaceItem{
		Team: "TRT", Kind: slo.KindPayloadStreams, ItemKey: "was-expired", GroupKey: "nightly",
		Outcome: "Accepted", OccurredAt: now.Add(-time.Minute),
	})
	require.NoError(t, err)

	pruner := NewTRTPayloadPruner(sloConfigManager(t, sloTeamConfig(2)), repo, time.Minute, logrus.New())
	pruner.prune()

	var kept *types.SLOWorkspaceItem
	for i := range repo.Items {
		if repo.Items[i].ItemKey == "was-expired" {
			kept = &repo.Items[i]
		}
	}
	require.NotNil(t, kept)
	assert.Equal(t, "Accepted", kept.Outcome)
}

func sloTeamConfig(recent int) *types.DashboardConfig {
	return &types.DashboardConfig{
		TeamSLOs: []types.TeamSLOConfig{{
			Team: "TRT",
			SLOs: []types.NamedSLO{{
				Name:   "accepted-payload-per-day",
				Source: slo.SourcePayloadAcceptance,
				Workspace: &types.SLOWorkspace{
					Kind:           slo.KindPayloadStreams,
					SchemaVersion:  1,
					RecentPayloads: recent,
					Streams:        []types.SLOStream{{Name: "nightly"}},
				},
			}},
		}},
	}
}

func sloConfigManager(t *testing.T, cfg *types.DashboardConfig) *config.Manager[types.DashboardConfig] {
	t.Helper()
	manager, err := config.NewManager("", func(string) (*types.DashboardConfig, error) {
		return cfg, nil
	}, logrus.New(), time.Second)
	require.NoError(t, err)
	return manager
}

func newSLOHandlers(t *testing.T, cfg *types.DashboardConfig, repo repositories.SLOWorkspaceRepository) *Handlers {
	t.Helper()
	return NewHandlers(logrus.New(), sloConfigManager(t, cfg), &outage.MockOutageManager{}, &repositories.MockComponentPingRepository{}, &repositories.MockTriageNoteRepository{}, &repositories.MockOutageLinkRepository{}, repo, nil)
}
