package slo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"ship-status-dash/pkg/types"
)

func TestEvaluatePayloadAcceptance(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	team := &types.TeamSLOConfig{
		Team: "TRT",
		SLOs: []types.NamedSLO{{
			Name:        "accepted-payload-per-day",
			DisplayName: "1 accepted payload per day",
			Source:      SourcePayloadAcceptance,
			Workspace: &types.SLOWorkspace{
				Kind:          KindPayloadStreams,
				SchemaVersion: 1,
				Streams: []types.SLOStream{
					{Name: "5.1.0-0.nightly"},
					{Name: "5.0.0-0.nightly"},
				},
			},
		}},
	}
	oldAccepted := now.Add(-32 * time.Hour)
	items := []types.SLOWorkspaceItem{
		{Kind: KindPayloadStreams, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(-6 * time.Hour)},
		{Kind: KindPayloadStreams, GroupKey: "5.1.0-0.nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
		{Kind: KindPayloadStreams, GroupKey: "5.0.0-0.nightly", Outcome: "Accepted", OccurredAt: oldAccepted},
		{Kind: KindPayloadStreams, GroupKey: "5.0.0-0.nightly", Outcome: "Rejected", OccurredAt: now.Add(-3 * time.Hour)},
		{Kind: KindPayloadStreams, GroupKey: "4.19-gone", Outcome: "Accepted", OccurredAt: now.Add(-time.Hour)},
	}

	got := Evaluate(now, team, items)
	require.Len(t, got, 1)
	assert.False(t, got[0].Met)
	assert.Equal(t, "24h", got[0].Window)
	assert.Equal(t, 1, got[0].Target.MinAccepted)
	require.Len(t, got[0].Groups, 2)
	assert.True(t, got[0].Groups[0].Met)
	assert.Equal(t, 1, got[0].Groups[0].Accepted)
	assert.False(t, got[0].Groups[1].Met)
	assert.Equal(t, 0, got[0].Groups[1].Accepted)
	require.NotNil(t, got[0].Groups[1].LastAcceptedAt)
	assert.True(t, got[0].Groups[1].LastAcceptedAt.Equal(oldAccepted))
}

func TestEvaluateIgnoresFuturePayloads(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	team := &types.TeamSLOConfig{
		Team: "TRT",
		SLOs: []types.NamedSLO{{
			Name:   "accepted-payload-per-day",
			Source: SourcePayloadAcceptance,
			Workspace: &types.SLOWorkspace{
				Kind:          KindPayloadStreams,
				SchemaVersion: SchemaVersionV1,
				Streams: []types.SLOStream{
					{Name: "5.1.0-0.nightly"},
					{Name: "5.0.0-0.nightly"},
				},
			},
		}},
	}
	inWindow := now.Add(-2 * time.Hour)
	items := []types.SLOWorkspaceItem{
		{Kind: KindPayloadStreams, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(48 * time.Hour)},
		{Kind: KindPayloadStreams, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: inWindow},
		{Kind: KindPayloadStreams, GroupKey: "5.1.0-0.nightly", Outcome: "Accepted", OccurredAt: now},
		{Kind: KindPayloadStreams, GroupKey: "5.0.0-0.nightly", Outcome: "Accepted", OccurredAt: now.Add(time.Hour)},
	}

	got := Evaluate(now, team, items)
	require.Len(t, got, 1)
	assert.False(t, got[0].Met)
	assert.True(t, got[0].Groups[0].Met)
	assert.Equal(t, 2, got[0].Groups[0].Accepted)
	require.NotNil(t, got[0].Groups[0].LastAcceptedAt)
	assert.True(t, got[0].Groups[0].LastAcceptedAt.Equal(now))
	assert.False(t, got[0].Groups[1].Met)
	assert.Equal(t, 0, got[0].Groups[1].Accepted)
	assert.Nil(t, got[0].Groups[1].LastAcceptedAt)
}

func TestEvaluateIgnoresUnknownSource(t *testing.T) {
	team := &types.TeamSLOConfig{
		SLOs: []types.NamedSLO{{Name: "other", Source: "prometheus"}},
	}
	assert.Empty(t, Evaluate(time.Now(), team, nil))
}

func TestPruneKeepsLastAcceptedAndWindow(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	items := []types.SLOWorkspaceItem{
		{Model: gorm.Model{ID: 1}, ItemKey: "a", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-1 * time.Hour)},
		{Model: gorm.Model{ID: 2}, ItemKey: "b", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-2 * time.Hour)},
		{Model: gorm.Model{ID: 3}, ItemKey: "c", GroupKey: "nightly", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
		{Model: gorm.Model{ID: 4}, ItemKey: "d", GroupKey: "nightly", Outcome: "Rejected", OccurredAt: now.Add(-72 * time.Hour)},
		{Model: gorm.Model{ID: 5}, ItemKey: "e", GroupKey: "removed", Outcome: "Accepted", OccurredAt: now.Add(-48 * time.Hour)},
		{Model: gorm.Model{ID: 6}, ItemKey: "f", GroupKey: "removed", Outcome: "Rejected", OccurredAt: now.Add(-1 * time.Hour)},
	}
	drop := TRTPayloadPruneIDs(now, []string{"nightly"}, 2, items)
	assert.ElementsMatch(t, []uint{4, 5}, drop)

	kept := DropItems(items, drop)
	display := TRTPayloadDisplayItems([]string{"nightly"}, 2, kept)
	require.Len(t, display, 2)
	assert.Equal(t, uint(1), display[0].ID)
	assert.Equal(t, uint(2), display[1].ID)
	assert.ElementsMatch(t, []string{"a", "b", "c"}, TRTPayloadItemKeys([]string{"nightly"}, kept))
}
