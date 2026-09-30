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
	return payloadWorkspaceMin(1, streams...)
}

func payloadWorkspaceMin(min int, streams ...string) *types.SLOWorkspace {
	settings := payloadv1.Settings{Window: "24h", MinAccepted: min, RecentPayloads: 2}
	for _, name := range streams {
		settings.Streams = append(settings.Streams, payloadv1.Stream{ReleaseController: "amd64", Name: name})
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		panic(err)
	}
	return &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: payloadv1.SchemaVersion, Spec: raw}
}

func at(t time.Time) *time.Time {
	return &t
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

	cutoff := now.Add(-24 * time.Hour)
	justBefore := cutoff.Add(-time.Millisecond)

	tests := []struct {
		name         string
		team         *types.TeamSLOConfig
		items        []types.SLOWorkspaceItem
		wantLen      int
		wantMet      bool
		wantAccepted []int
		wantGroupMet []bool
		wantLast     []*time.Time
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
			wantLen: 1, wantMet: false, wantWindow: "24h", wantMin: 1, wantAccepted: []int{1, 0}, wantGroupMet: []bool{true, false},
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
			wantLen: 1, wantMet: false, wantAccepted: []int{2, 0}, wantGroupMet: []bool{true, false},
			wantLast: []*time.Time{at(now), nil},
		},
		{
			name: "meets min_accepted of 2",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspaceMin(2, "nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-3 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-time.Minute)},
			},
			wantLen: 1, wantMet: true, wantWindow: "24h", wantMin: 2, wantAccepted: []int{2}, wantGroupMet: []bool{true},
		},
		{
			name: "misses when accepted count is below min_accepted",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspaceMin(2, "nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: inWindow},
			},
			wantLen: 1, wantMet: false, wantWindow: "24h", wantMin: 2, wantAccepted: []int{1}, wantGroupMet: []bool{false},
		},
		{
			name: "counts one above min_accepted",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspaceMin(2, "nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-5 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-4 * time.Hour)},
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-3 * time.Hour)},
			},
			wantLen: 1, wantMet: true, wantMin: 2, wantAccepted: []int{3}, wantGroupMet: []bool{true},
		},
		{
			name: "counts the cutoff and records an older miss as last accepted",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspace("at-cutoff", "before-cutoff"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "at-cutoff", Outcome: "Accepted", OccurredAt: cutoff},
				{Kind: payloadv1.Kind, GroupKey: "before-cutoff", Outcome: "Accepted", OccurredAt: justBefore},
				{Kind: payloadv1.Kind, GroupKey: "at-cutoff", Outcome: "Accepted", OccurredAt: now.Add(time.Hour)},
			},
			wantLen: 1, wantMet: false, wantAccepted: []int{1, 0}, wantGroupMet: []bool{true, false},
			wantLast: []*time.Time{at(cutoff), at(justBefore)},
		},
		{
			name: "counts a payload at now",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspace("nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now},
			},
			wantLen: 1, wantMet: true, wantAccepted: []int{1}, wantGroupMet: []bool{true},
			wantLast: []*time.Time{at(now)},
		},
		{
			name: "ignores a future payload for the count and last accepted time",
			team: &types.TeamSLOConfig{Team: "TRT", SLOs: []types.NamedSLO{{
				Name: "accepted-payload-per-day", Source: payloadv1.Source,
				Workspace: payloadWorkspace("nightly"),
			}}},
			items: []types.SLOWorkspaceItem{
				{Kind: payloadv1.Kind, GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(time.Hour)},
			},
			wantLen: 1, wantMet: false, wantAccepted: []int{0}, wantGroupMet: []bool{false},
			wantLast: []*time.Time{nil},
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
			require.Len(t, tt.wantGroupMet, len(tt.wantAccepted))
			for i, accepted := range tt.wantAccepted {
				assert.Equal(t, accepted, result.Groups[i].Accepted)
				assert.Equal(t, tt.wantGroupMet[i], result.Groups[i].Met)
			}
			if tt.wantLast != nil {
				require.Len(t, tt.wantLast, len(result.Groups))
				for i, want := range tt.wantLast {
					got := result.Groups[i].LastAcceptedAt
					if want == nil {
						assert.Nil(t, got)
						continue
					}
					require.NotNil(t, got)
					assert.True(t, got.Equal(*want), "group %d last accepted %s, want %s", i, got, want)
				}
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

	shuffled := append([]types.SLOWorkspaceItem(nil), items...)
	for i, j := 0, len(shuffled)-1; i < j; i, j = i+1, j-1 {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}
	display := payloadv1.DisplayItems(settings, shuffled)
	require.Len(t, display, 2)
	assert.Equal(t, uint(1), display[0].ID)
	assert.Equal(t, uint(2), display[1].ID)

	same := now.Add(-time.Hour)
	tieSettings := payloadv1.Settings{
		Window: "24h", MinAccepted: 1, RecentPayloads: 2,
		Streams: []payloadv1.Stream{
			{ReleaseController: "amd64", Name: "nightly"},
			{ReleaseController: "amd64", Name: "ci"},
		},
	}
	tied := []types.SLOWorkspaceItem{
		{Model: gorm.Model{ID: 2}, ItemKey: "low", GroupKey: "nightly", OccurredAt: same},
		{Model: gorm.Model{ID: 1}, ItemKey: "other", GroupKey: "ci", OccurredAt: now.Add(-time.Minute)},
		{Model: gorm.Model{ID: 8}, ItemKey: "high", GroupKey: "nightly", OccurredAt: same},
		{Model: gorm.Model{ID: 4}, ItemKey: "newest", GroupKey: "nightly", OccurredAt: now},
	}
	ordered := payloadv1.DisplayItems(tieSettings, tied)
	require.Len(t, ordered, 3)
	assert.Equal(t, []uint{4, 8, 1}, []uint{ordered[0].ID, ordered[1].ID, ordered[2].ID})
}
