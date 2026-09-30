package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/repositories"
	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

func TestSLOPayloadRetention(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name     string
		recent   int
		items    []types.SLOWorkspaceItem
		upsert   *types.SLOWorkspaceItem
		wantKeys []string
	}{
		{
			name:   "pruner deletes expired rows and keeps the newest accepted",
			recent: 2,
			items: []types.SLOWorkspaceItem{
				{Model: gorm.Model{ID: 1}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "old-rejected", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
				{Model: gorm.Model{ID: 2}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "recent", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
				{Model: gorm.Model{ID: 3}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "newer", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-time.Hour)},
				{Model: gorm.Model{ID: 4}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "kept-accepted", GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
			},
			wantKeys: []string{"recent", "newer", "kept-accepted"},
		},
		{
			name:   "upsert before prune keeps a row that is no longer expired",
			recent: 2,
			items: []types.SLOWorkspaceItem{
				{Model: gorm.Model{ID: 1}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "was-expired", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
				{Model: gorm.Model{ID: 2}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "recent", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
				{Model: gorm.Model{ID: 3}, Team: "TRT", Kind: payloadv1.Kind, ItemKey: "newer", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-time.Hour)},
			},
			upsert: &types.SLOWorkspaceItem{
				Team: "TRT", Kind: payloadv1.Kind, ItemKey: "was-expired", GroupKey: "nightly",
				Outcome: "Accepted", OccurredAt: now.Add(-time.Minute),
			},
			wantKeys: []string{"was-expired", "recent", "newer"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &repositories.MockSLOWorkspaceRepository{Items: append([]types.SLOWorkspaceItem(nil), tt.items...)}
			if tt.upsert != nil {
				_, err := repo.UpsertItem(tt.upsert)
				require.NoError(t, err)
			}

			pruner := NewTRTSLOPayloadPruner(sloConfigManager(t, sloTeamConfig(tt.recent)), repo, time.Minute, logrus.New())
			pruner.prune()
			var keys []string
			for _, item := range repo.Items {
				keys = append(keys, item.ItemKey)
			}
			assert.ElementsMatch(t, tt.wantKeys, keys)
			if tt.upsert != nil {
				var kept *types.SLOWorkspaceItem
				for i := range repo.Items {
					if repo.Items[i].ItemKey == tt.upsert.ItemKey {
						kept = &repo.Items[i]
					}
				}
				require.NotNil(t, kept)
				assert.Equal(t, tt.upsert.Outcome, kept.Outcome)
			}
		})
	}
}

func sloTeamConfig(recent int) *types.DashboardConfig {
	raw, err := json.Marshal(payloadv1.Settings{
		Window: "24h", MinAccepted: 1, RecentPayloads: recent,
		Streams: []payloadv1.Stream{{ReleaseController: "amd64", Name: "nightly"}},
	})
	if err != nil {
		panic(err)
	}
	return &types.DashboardConfig{
		TeamSLOs: []types.TeamSLOConfig{{
			Team: "TRT",
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

func sloConfigManager(t *testing.T, cfg *types.DashboardConfig) *config.Manager[types.DashboardConfig] {
	t.Helper()
	manager, err := config.NewManager("", func(string) (*types.DashboardConfig, error) {
		return cfg, nil
	}, logrus.New(), time.Second)
	require.NoError(t, err)
	return manager
}
