package seed

import (
	"encoding/json"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ship-status-dash/pkg/slo"
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
	component, sub, err := SLOComponent(cfg, "TRT")
	require.NoError(t, err)
	assert.Equal(t, "trt-incidents", component)
	assert.Equal(t, "incidents", sub)

	_, _, err = SLOComponent(cfg, "Nope")
	require.Error(t, err)
}

func TestPayloadWorkspaceUsesConfig(t *testing.T) {
	cfg := &types.DashboardConfig{
		TeamSLOs: []types.TeamSLOConfig{{
			Team: "Widget",
			SLOs: []types.NamedSLO{{
				Name:   "payloads",
				Source: slo.SourcePayloadAcceptance,
				Workspace: &types.SLOWorkspace{
					Kind:          slo.KindPayloadStreams,
					SchemaVersion: slo.SchemaVersionV1,
					Streams:       []types.SLOStream{{Controller: "arm64", Name: "renamed-nightly"}},
				},
			}},
		}},
	}
	team, ws, err := PayloadWorkspace(cfg)
	require.NoError(t, err)
	assert.Equal(t, "Widget", team)
	require.Len(t, ws.Streams, 1)
	assert.Equal(t, "renamed-nightly", ws.Streams[0].Name)
}

func TestRoleAtMatchesE2EStreamCount(t *testing.T) {
	assert.Equal(t, RoleRecentReject, RoleAt(2, 0))
	assert.Equal(t, RoleMiss, RoleAt(2, 1))
	assert.Equal(t, []int{0, 1}, JiraStreamIndexes(2))
	assert.Equal(t, 1, MissIndex(2))
}

func TestBuildSeed(t *testing.T) {
	now := time.Date(2026, 9, 29, 16, 0, 0, 0, time.UTC)
	streams := []types.SLOStream{
		{Controller: "amd64", Name: "5.1.0-0.nightly"},
		{Controller: "amd64", Name: "5.1.0-0.ci"},
		{Controller: "amd64", Name: "5.0.0-0.nightly"},
		{Controller: "amd64", Name: "5.0.0-0.ci"},
		{Controller: "arm64", Name: "renamed-0.nightly"},
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
		assert.Equal(t, slo.KindPayloadStreams, item.Kind)
		assert.Equal(t, slo.SchemaVersionV1, item.SchemaVersion)
		assert.Equal(t, UpdatedBy, item.UpdatedBy)
		assert.False(t, seenKey[item.ItemKey])
		seenKey[item.ItemKey] = true
		require.NoError(t, slo.ValidateDetails(item.Kind, item.SchemaVersion, item.Details))
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

	team := &types.TeamSLOConfig{
		Team: "Widget",
		SLOs: []types.NamedSLO{{
			Name:   "accepted-payload-per-day",
			Source: slo.SourcePayloadAcceptance,
			Workspace: &types.SLOWorkspace{
				Kind:          slo.KindPayloadStreams,
				SchemaVersion: slo.SchemaVersionV1,
				Streams:       streams,
			},
		}},
	}
	got := slo.Evaluate(now, team, items)
	require.Len(t, got, 1)
	assert.False(t, got[0].Met)

	byStream := map[string]slo.GroupEval{}
	for _, group := range got[0].Groups {
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

	names := make([]string, len(streams))
	for i, stream := range streams {
		names[i] = stream.Name
	}
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
	assert.Contains(t, slo.TRTPayloadPruneIDs(now, names, 5, items), candidateID)
	assert.NotContains(t, itemKeysOf(slo.TRTPayloadDisplayItems(names, 5, items)), PruneCandidateItemKey)
	// Twelve hours later the candidate is still a day outside the window.
	assert.Contains(t, slo.TRTPayloadPruneIDs(now.Add(12*time.Hour), names, 5, items), candidateID)
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

func jobsOf(t *testing.T, item types.SLOWorkspaceItem) []slo.PayloadJobV1 {
	t.Helper()
	var doc slo.PayloadDetailsV1
	require.NoError(t, json.Unmarshal(item.Details, &doc))
	return doc.Jobs
}

func failedJobNamed(jobs []slo.PayloadJobV1, name string) bool {
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
