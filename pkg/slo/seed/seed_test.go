package seed

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ship-status-dash/pkg/slo"
	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

func TestSLOComponentUsesListedSlug(t *testing.T) {
	cfg := &types.DashboardConfig{
		Components: []*types.Component{
			{
				Name: "TRT Incidents", ShipTeam: "Nope", SLOComponent: true,
				Subcomponents: []types.SubComponent{{Name: "Incidents"}},
			},
			{
				Name: "Other", ShipTeam: "TRT", SLOComponent: true,
				Subcomponents: []types.SubComponent{{Name: "Ignored"}},
			},
		},
		TeamSLOs: []types.TeamSLOConfig{{
			Team:          "TRT",
			SLOComponents: []string{"trt-incidents"},
		}},
	}
	cfg.AssignSlugs()

	tests := []struct {
		team    string
		want    string
		wantSub string
		wantErr bool
	}{
		{team: "TRT", want: "trt-incidents", wantSub: "incidents"},
		{team: "Nope", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.team, func(t *testing.T) {
			component, sub, err := SLOComponent(cfg, tt.team)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, component)
			assert.Equal(t, tt.wantSub, sub)
		})
	}
}

func TestTRTPayloadSettingsUsesConfig(t *testing.T) {
	raw, err := json.Marshal(payloadv1.Settings{
		Window: "24h", MinAccepted: 1, RecentPayloads: 2,
		Streams: []payloadv1.Stream{{ReleaseController: "arm64", Name: "renamed-nightly"}},
	})
	require.NoError(t, err)
	cfg := &types.DashboardConfig{
		TeamSLOs: []types.TeamSLOConfig{{
			Team: "Widget",
			SLOs: []types.NamedSLO{{
				Name:   "payloads",
				Source: payloadv1.Source,
				Workspace: &types.SLOWorkspace{
					Kind:          payloadv1.Kind,
					SchemaVersion: payloadv1.SchemaVersion,
					Spec:          raw,
				},
			}},
		}},
	}
	team, settings, err := TRTPayloadSettings(cfg)
	require.NoError(t, err)
	assert.Equal(t, "Widget", team)
	require.Len(t, settings.Streams, 1)
	assert.Equal(t, "renamed-nightly", settings.Streams[0].Name)
	assert.Equal(t, "arm64", settings.Streams[0].ReleaseController)
}

func TestRoleAt(t *testing.T) {
	tests := []struct {
		n        int
		index    int
		wantRole string
		wantMiss int
	}{
		{n: 2, index: 0, wantRole: RoleRecentReject, wantMiss: 1},
		{n: 2, index: 1, wantRole: RoleMiss, wantMiss: 1},
		{n: 4, index: 2, wantRole: RoleMiss, wantMiss: 2},
	}
	for _, tt := range tests {
		t.Run(tt.wantRole, func(t *testing.T) {
			assert.Equal(t, tt.wantRole, RoleAt(tt.n, tt.index))
			assert.Equal(t, tt.wantMiss, MissIndex(tt.n))
		})
	}
	assert.Equal(t, []int{0, 1}, JiraStreamIndexes(2))
}

func TestBuildSeed(t *testing.T) {
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	streams := []payloadv1.Stream{
		{ReleaseController: "amd64", Name: "5.1.0-0.nightly"},
		{ReleaseController: "amd64", Name: "5.1.0-0.ci"},
		{ReleaseController: "amd64", Name: "5.0.0-0.nightly"},
		{ReleaseController: "amd64", Name: "5.0.0-0.ci"},
		{ReleaseController: "arm64", Name: "renamed-0.nightly"},
	}

	seeded, err := buildSeed(now, "Widget", streams, 5)
	require.NoError(t, err)
	require.Len(t, seeded, len(streams)*5+1)
	attachSampleLinks(seeded, streams, "/trt-incidents/incidents/outages/7", 7)

	items := make([]types.SLOWorkspaceItem, 0, len(seeded))
	jiraLinks := 0
	outageLinks := 0
	seenKey := map[string]bool{}
	for _, row := range seeded {
		item := row.item
		assert.Equal(t, "Widget", item.Team)
		assert.Equal(t, payloadv1.Kind, item.Kind)
		assert.Equal(t, payloadv1.SchemaVersion, item.SchemaVersion)
		assert.Equal(t, UpdatedBy, item.UpdatedBy)
		assert.False(t, seenKey[item.ItemKey])
		seenKey[item.ItemKey] = true
		require.NoError(t, payloadv1.ValidateDetails(item.Details))
		if item.ItemKey == PruneCandidateItemKey {
			assert.Equal(t, streams[0].Name, item.GroupKey)
			assert.Equal(t, "Rejected", item.Outcome)
			assert.True(t, item.OccurredAt.Equal(now.Add(-PruneCandidateAgo)))
		} else {
			assert.Contains(t, string(item.Details), item.ItemKey)
		}
		items = append(items, item)
		for _, link := range row.links {
			switch link.LinkType {
			case "jira":
				jiraLinks++
				assert.Equal(t, JiraURL, link.URL)
			case "outage":
				outageLinks++
				assert.Equal(t, "/trt-incidents/incidents/outages/7", link.URL)
				require.NotNil(t, link.OutageID)
				assert.Equal(t, uint(7), *link.OutageID)
			default:
				t.Fatalf("unexpected link type %s", link.LinkType)
			}
		}
	}
	assert.Equal(t, 2, jiraLinks)
	assert.Equal(t, 1, outageLinks)
	assertConsistentStreaks(t, items)

	spec, err := json.Marshal(payloadv1.Settings{Window: "24h", MinAccepted: 1, RecentPayloads: 5, Streams: streams})
	require.NoError(t, err)
	team := &types.TeamSLOConfig{
		Team: "Widget",
		SLOs: []types.NamedSLO{{
			Name:   "accepted-payload-per-day",
			Source: payloadv1.Source,
			Workspace: &types.SLOWorkspace{
				Kind:          payloadv1.Kind,
				SchemaVersion: payloadv1.SchemaVersion,
				Spec:          spec,
			},
		}},
	}
	got, err := slo.Evaluate(now, team, items)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Met)
	var result payloadv1.Result
	require.NoError(t, json.Unmarshal(got[0].Result, &result))

	byStream := map[string]payloadv1.GroupEval{}
	for _, group := range result.Groups {
		byStream[group.Key] = group
	}
	assert.True(t, byStream[streams[0].Name].Met)
	assert.Equal(t, "Rejected", newest(items, streams[0].Name).Outcome)
	assert.Contains(t, string(newest(items, streams[0].Name).Details), "https://amd64.ocp.releases.ci.openshift.org/")
	assert.True(t, byStream[streams[1].Name].Met)
	assert.True(t, byStream[streams[3].Name].Met)
	assert.True(t, byStream[streams[4].Name].Met)
	assert.Contains(t, string(newest(items, streams[4].Name).Details), "https://arm64.ocp.releases.ci.openshift.org/")
	assert.False(t, byStream[streams[2].Name].Met)
	assert.Equal(t, 0, byStream[streams[2].Name].Accepted)
	require.NotNil(t, byStream[streams[2].Name].LastAcceptedAt)
	assert.True(t, byStream[streams[2].Name].LastAcceptedAt.Equal(now.Add(-32*time.Hour)))
	assert.True(t, byStream[streams[0].Name].LastAcceptedAt.Equal(now.Add(-6*time.Hour)))
	assert.True(t, byStream[streams[1].Name].LastAcceptedAt.Equal(now.Add(-4*time.Hour)))
	assert.True(t, byStream[streams[3].Name].LastAcceptedAt.Equal(now.Add(-8*time.Hour)))

	for i := range items {
		items[i].ID = uint(i + 1)
	}
	var candidateID uint
	for _, item := range items {
		if item.ItemKey == PruneCandidateItemKey {
			candidateID = item.ID
		}
	}
	require.NotZero(t, candidateID)
	settings := payloadv1.Settings{Window: "24h", MinAccepted: 1, RecentPayloads: 5, Streams: streams}
	assert.Contains(t, payloadv1.PruneIDs(now, settings, items), candidateID)
	assert.NotContains(t, itemKeysOf(payloadv1.DisplayItems(settings, items)), PruneCandidateItemKey)
	// Twelve hours later the candidate is still a day outside the window.
	assert.Contains(t, payloadv1.PruneIDs(now.Add(12*time.Hour), settings, items), candidateID)
}

func itemKeysOf(items []types.SLOWorkspaceItem) []string {
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = item.ItemKey
	}
	return keys
}

func assertConsistentStreaks(t *testing.T, items []types.SLOWorkspaceItem) {
	t.Helper()
	byStream := map[string][]types.SLOWorkspaceItem{}
	for _, item := range items {
		byStream[item.GroupKey] = append(byStream[item.GroupKey], item)
	}
	for stream, rows := range byStream {
		sort.Slice(rows, func(i, j int) bool {
			return rows[i].OccurredAt.After(rows[j].OccurredAt)
		})
		if rows[0].Outcome == "Accepted" {
			for _, row := range rows {
				for _, job := range jobsOf(t, row) {
					assert.Nil(t, job.RecurringCount, "%s %s keeps a streak after an accepted payload", stream, row.ItemKey)
				}
			}
		}
		for i, row := range rows {
			for _, job := range jobsOf(t, row) {
				if job.RecurringCount == nil || *job.RecurringCount < 2 {
					continue
				}
				for k := 0; k < *job.RecurringCount; k++ {
					require.Less(t, i+k, len(rows), "%s streak walks off the stream", stream)
					assert.NotEqual(t, "Accepted", rows[i+k].Outcome)
					assert.True(t, failedJobNamed(jobsOf(t, rows[i+k]), job.Name), "%s streak includes a payload that did not fail %s", stream, job.Name)
				}
			}
		}
	}
}

func jobsOf(t *testing.T, item types.SLOWorkspaceItem) []payloadv1.PayloadJob {
	t.Helper()
	var doc payloadv1.PayloadDetails
	require.NoError(t, json.Unmarshal(item.Details, &doc))
	return doc.Jobs
}

func failedJobNamed(jobs []payloadv1.PayloadJob, name string) bool {
	for _, job := range jobs {
		if job.Name == name && job.State == "failure" {
			return true
		}
	}
	return false
}

func newest(items []types.SLOWorkspaceItem, stream string) types.SLOWorkspaceItem {
	var latest types.SLOWorkspaceItem
	for _, item := range items {
		if item.GroupKey != stream {
			continue
		}
		if latest.ItemKey == "" || item.OccurredAt.After(latest.OccurredAt) {
			latest = item
		}
	}
	return latest
}
