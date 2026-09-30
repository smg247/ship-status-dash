package slo

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

func payloadWorkspace(streams ...string) *types.SLOWorkspace {
	settings := payloadv1.Settings{Window: "24h", MinAccepted: 1, RecentPayloads: 2}
	for _, name := range streams {
		settings.Streams = append(settings.Streams, payloadv1.Stream{ReleaseController: "amd64", Name: name})
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		panic(err)
	}
	return &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: payloadv1.SchemaVersion, Spec: raw}
}

func payloadResult(t *testing.T, ev Evaluation) payloadv1.Result {
	t.Helper()
	var result payloadv1.Result
	require.NoError(t, json.Unmarshal(ev.Result, &result))
	return result
}

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	oldAccepted := now.Add(-32 * time.Hour)
	inWindow := now.Add(-2 * time.Hour)

	tests := []struct {
		name         string
		team         *types.TeamSLOConfig
		items        []types.SLOWorkspaceItem
		wantLen      int
		wantMet      bool
		wantAccepted []int
		wantWindow   string
		wantMin      int
	}{
		{
			name: "misses a stream outside the window",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", DisplayName: "1 accepted payload per day", Source: payloadv1.Source,
				Workspace: payloadWorkspace("5.1.0-0.nightly", "5.0.0-0.nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(-6 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "5.1.0-0.nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "5.0.0-0.nightly", Outcome: "Accepted", OccurredAt: oldAccepted},
				{Kind: payloadv1.Kind, GroupKey: "5.0.0-0.nightly", Outcome: "Rejected", OccurredAt: now.Add(-3 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "4.19-gone", Outcome: "Accepted", OccurredAt: now.Add(-time.Hour)},
			},
			wantLen: 1, wantWindow: "24h", wantMin: 1, wantAccepted: []int{1, 0},
		},
		{
			name: "ignores future payloads",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspace("5.1.0-0.nightly", "5.0.0-0.nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(48 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: inWindow},
				{Kind: payloadv1.Kind, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now},
				{Kind: payloadv1.Kind, GroupKey: "5.0.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(time.Hour)},
			},
			wantLen: 1, wantAccepted: []int{2, 0},
		},
		{
			name:    "ignores unknown source",
			team:    &types.TeamSLOConfig{SLOs: []types.NamedSLO{{Name: "other", Source: "prometheus"}}},
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(now, tt.team, tt.items)
			require.NoError(t, err)
			require.Len(t, got, tt.wantLen)
			if tt.wantLen == 0 {
				return
			}
			assert.Equal(t, tt.wantMet, got[0].Met)
			result := payloadResult(t, got[0])
			if tt.wantWindow != "" {
				assert.Equal(t, tt.wantWindow, result.Window)
				assert.Equal(t, tt.wantMin, result.Target.MinAccepted)
			}
			require.Len(t, result.Groups, len(tt.wantAccepted))
			for i, accepted := range tt.wantAccepted {
				assert.Equal(t, accepted, result.Groups[i].Accepted)
				assert.Equal(t, accepted > 0, result.Groups[i].Met)
			}
		})
	}
}

func TestPruneAndDisplay(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	items := []types.SLOWorkspaceItem{
		{Model: gorm.Model{ID: 1}, ItemKey: "a", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-1 * time.Hour)},
		{Model: gorm.Model{ID: 2}, ItemKey: "b", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
		{Model: gorm.Model{ID: 3}, ItemKey: "c", GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
		{Model: gorm.Model{ID: 4}, ItemKey: "d", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
		{Model: gorm.Model{ID: 5}, ItemKey: "e", GroupKey: "removed", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
		{Model: gorm.Model{ID: 6}, ItemKey: "f", GroupKey: "removed", Outcome: "Rejected", OccurredAt: now.Add(-1 * time.Hour)},
	}
	settings := payloadv1.Settings{
		Window: "24h", MinAccepted: 1, RecentPayloads: 2,
		Streams: []payloadv1.Stream{{ReleaseController: "amd64", Name: "nightly"}},
	}
	ws := &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: payloadv1.SchemaVersion}
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	ws.Spec = raw

	drop, err := PruneIDs(now, ws, items)
	require.NoError(t, err)
	assert.ElementsMatch(t, []uint{4, 5}, drop)

	display := payloadv1.DisplayItems(settings, items)
	require.Len(t, display, 2)
	assert.Equal(t, uint(1), display[0].ID)
	assert.Equal(t, uint(2), display[1].ID)
}
