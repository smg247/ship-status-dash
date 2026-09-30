package e2e

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"

	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/slo/seed"
	"ship-status-dash/pkg/types"
)

type e2eItem struct {
	ID         uint                     `json:"id"`
	ItemKey    string                   `json:"item_key"`
	GroupKey   string                   `json:"group_key"`
	OccurredAt time.Time                `json:"occurred_at"`
	Outcome    string                   `json:"outcome"`
	Details    json.RawMessage          `json:"details"`
	Notes      string                   `json:"notes"`
	UpdatedBy  string                   `json:"updated_by"`
	Links      []types.SLOWorkspaceLink `json:"links"`
}

type e2eGroup struct {
	Key            string     `json:"key"`
	Accepted       int        `json:"accepted"`
	Met            bool       `json:"met"`
	LastAcceptedAt *time.Time `json:"last_accepted_at,omitempty"`
}

type e2eResult struct {
	Window string `json:"window"`
	Target struct {
		MinAccepted int `json:"min_accepted"`
	} `json:"target"`
	Groups []e2eGroup `json:"groups"`
}

type e2eEvaluation struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Source      string          `json:"source"`
	Met         bool            `json:"met"`
	Result      json.RawMessage `json:"result"`
}

type e2eWorkspace struct {
	Kind          string          `json:"kind"`
	SchemaVersion int             `json:"schema_version"`
	Spec          json.RawMessage `json:"spec"`
}

type e2eComponent struct {
	Component    string         `json:"component"`
	SubComponent string         `json:"sub_component"`
	Outages      []types.Outage `json:"outages"`
}

type e2eTeamView struct {
	Team          string          `json:"team"`
	Workspace     *e2eWorkspace   `json:"workspace"`
	Evaluations   []e2eEvaluation `json:"evaluations"`
	SLOComponents []e2eComponent  `json:"slo_components"`
	Items         []e2eItem       `json:"items"`
}

type e2eSummary struct {
	Teams []struct {
		Team          string          `json:"team"`
		Evaluations   []e2eEvaluation `json:"evaluations"`
		SLOComponents []e2eComponent  `json:"slo_components"`
	} `json:"teams"`
}

const (
	sloTeam          = "TRT"
	trtPayloadKind   = "payload_streams"
	trtStreamNightly = "5.1.0-0.nightly"
	trtStreamCI      = "5.1.0-0.ci"
	sloItemsPath     = "/api/teams/" + sloTeam + "/slo/items"
)

func TestE2E_TeamSLO(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	serverURL := os.Getenv("TEST_SERVER_URL")
	mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
	require.NotEmpty(t, serverURL)
	require.NotEmpty(t, mockOauthProxyURL)

	client, err := NewTestHTTPClient(serverURL, mockOauthProxyURL)
	require.NoError(t, err)

	t.Cleanup(func() { deleteWorkspaceItems(t, client) })

	t.Run("PublicRead", testSLOPublicRead(client))
	t.Run("PruneCandidate", testSeededPruneCandidate(client))
	t.Run("ListAPIsOmitSLOComponent", testSLOListAPIsOmitComponent(client))
	t.Run("Authorization", testSLOAuthorization(client))
	t.Run("DeveloperUpsert", testSLODeveloperUpsert(client))
	t.Run("SLOComponentOutage", testSLOComponentOutage(client))
	t.Run("MissDoesNotCreateOutage", testSLOMissDoesNotCreateOutage(client))

	t.Run("TRTPayloadStreams", func(t *testing.T) {
		t.Run("Workspace", testTRTPayloadStreamsWorkspace(client))
		t.Run("RejectsIncompleteDetails", testTRTPayloadStreamsRejectsIncompleteDetails(client))
		t.Run("Upsert", testTRTPayloadStreamsUpsert(client))
		t.Run("Evaluation", testTRTPayloadStreamsEvaluation(client))
		t.Run("ReplaceAndLink", testTRTPayloadStreamsReplaceAndLink(client))
		t.Run("Prune", testTRTPayloadStreamsPrune(client))
	})
}

func testSeededPruneCandidate(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		view := getTeamSLO(t, client)
		require.NotNil(t, view.Workspace)
		settings := workspaceSettings(t, view.Workspace)
		require.NotEmpty(t, settings.Streams)
		stream := settings.Streams[0].Name

		putSLOItemChai(t, client, mustSLOItemBody(t, seed.PruneCandidateItemKey, stream, "Rejected", time.Now().UTC().Add(-seed.PruneCandidateAgo), "", payloadDetails(t, "https://example.com/prune-candidate", nil)))
		require.Contains(t, itemKeys(getTeamSLO(t, client).Items), seed.PruneCandidateItemKey)

		// e2e starts the dashboard with --trt-payload-prune-interval=15s. 45s covers a tick that just fired.
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 45*time.Second, true, func(context.Context) (bool, error) {
			return !containsKey(itemKeys(getTeamSLO(t, client).Items), seed.PruneCandidateItemKey), nil
		})
		require.NoError(t, err, "pruner should delete %s", seed.PruneCandidateItemKey)
	}
}

func testSLOPublicRead(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		view := getTeamSLO(t, client)
		assertSeededWorkspace(t, view)

		summary := getSLOSummary(t, client)
		require.Len(t, summary.Teams, 1)
		assert.Equal(t, sloTeam, summary.Teams[0].Team)
		assertEvaluationsMatch(t, view.Evaluations, summary.Teams[0].Evaluations)
		assert.NotContains(t, summaryTeamKeys(t, client), "items")
		require.NotNil(t, seedOutageIn(summary.Teams[0].SLOComponents))

		unknown := getTeamSLOPath(t, client, "/api/teams/Nope/slo")
		assert.Equal(t, "Nope", unknown.Team)
		assert.Nil(t, unknown.Workspace)
		assert.Empty(t, unknown.Evaluations)
		assert.Empty(t, unknown.Items)
	}
}

func testSLOListAPIsOmitComponent(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		for _, component := range getComponents(t, client) {
			assert.NotEqual(t, trtIncidentsComponent, component.Name)
		}
		for _, sub := range getSubComponents(t, client, "", "", "TRT") {
			assert.NotEqual(t, trtIncidentsComponent, sub.ComponentName)
		}

		component := getComponent(t, client, trtIncidentsComponent)
		assert.Equal(t, trtIncidentsComponent, component.Name)
		assert.True(t, component.SLOComponent)
		require.Len(t, component.Subcomponents, 1)
		assert.Equal(t, trtIncidentsSub, component.Subcomponents[0].Name)
	}
}

func testSLOAuthorization(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		body := mustSLOItemBody(t, "e2e-unauth", trtStreamNightly, "Rejected", time.Now().UTC(), "nope", payloadDetails(t, "https://example.com/p", nil))

		t.Run("chai-bot without acting_for returns 400", func(t *testing.T) {
			resp, err := client.PutWithBearerToken(sloItemsPath, body, chaiBotSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("chai-bot acting for an unauthorized user returns 403", func(t *testing.T) {
			resp, err := client.PutWithBearerToken(sloItemsPath, body, chaiBotSAToken, "stranger")
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})

		t.Run("editor is not a team SLO owner", func(t *testing.T) {
			editor, err := NewTestHTTPClientWithUsername(client.publicURL, client.protectedURL, "editor")
			require.NoError(t, err)
			resp, err := editor.Put(sloItemsPath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})

		t.Run("unknown schema is rejected", func(t *testing.T) {
			bad := mustSLOItemBody(t, "e2e-bad-schema", trtStreamNightly, "Accepted", time.Now().UTC(), "", payloadDetails(t, "https://example.com/p", nil))
			var payload map[string]interface{}
			require.NoError(t, json.Unmarshal(bad, &payload))
			payload["schema_version"] = 99
			encoded, err := json.Marshal(payload)
			require.NoError(t, err)
			resp, err := client.PutWithBearerToken(sloItemsPath, encoded, chaiBotSAToken, "chai-bot")
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		keys := itemKeys(getTeamSLO(t, client).Items)
		assert.NotContains(t, keys, "e2e-unauth")
		assert.NotContains(t, keys, "e2e-bad-schema")
		assert.NotEmpty(t, keys)
	}
}

func testTRTPayloadStreamsWorkspace(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)

		view := getTeamSLO(t, client)
		require.NotNil(t, view.Workspace)
		assert.Equal(t, trtPayloadKind, view.Workspace.Kind)
		assert.Equal(t, 1, view.Workspace.SchemaVersion)
		settings := workspaceSettings(t, view.Workspace)
		assert.Equal(t, 2, settings.RecentPayloads)
		assert.Equal(t, "24h", settings.Window)
		require.Len(t, view.Evaluations, 1)
		ev := view.Evaluations[0]
		result := payloadResult(t, ev)
		assert.Equal(t, "accepted-payload-per-day", ev.Name)
		assert.Equal(t, "24h", result.Window)
		assert.Equal(t, 1, result.Target.MinAccepted)
		assert.False(t, ev.Met)
		require.Len(t, result.Groups, 2)
		assert.Equal(t, trtStreamNightly, result.Groups[0].Key)
		assert.Equal(t, trtStreamCI, result.Groups[1].Key)
		assert.False(t, result.Groups[0].Met)
		assert.Nil(t, result.Groups[0].LastAcceptedAt)
	}
}

func testTRTPayloadStreamsRejectsIncompleteDetails(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)
		bad := mustSLOItemBody(t, "e2e-bad-details", trtStreamNightly, "Accepted", time.Now().UTC(), "", json.RawMessage(`{"payload_url":"https://example.com/p"}`))
		resp, err := client.PutWithBearerToken(sloItemsPath, bad, chaiBotSAToken, "chai-bot")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		assert.Empty(t, getTeamSLO(t, client).Items)
	}
}

func testTRTPayloadStreamsUpsert(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)
		t.Cleanup(func() { deleteWorkspaceItems(t, client) })

		now := time.Now().UTC()
		countA := 2
		first := putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-a", trtStreamNightly, "Accepted", now.Add(-3*time.Hour), "first", payloadDetails(t, "https://example.com/a", &countA)))
		assert.Equal(t, "chai-bot", first.UpdatedBy)
		assert.Equal(t, 2, jobRecurringCount(t, first.Details))

		countB := 4
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-b", trtStreamNightly, "Rejected", now.Add(-2*time.Hour), "second", payloadDetails(t, "https://example.com/b", &countB)))
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-c", trtStreamNightly, "Rejected", now.Add(-time.Hour), "third", payloadDetails(t, "https://example.com/c", nil)))

		view := getTeamSLO(t, client)
		assert.ElementsMatch(t, []string{"e2e-a", "e2e-b", "e2e-c"}, itemKeys(view.Items))
		nightly := itemsForStream(view.Items, trtStreamNightly)
		require.Len(t, nightly, 3)
		assert.Equal(t, "e2e-c", nightly[0].ItemKey)
		assert.Equal(t, "e2e-b", nightly[1].ItemKey)
		storedB := itemByKey(t, view.Items, "e2e-b")
		require.NotNil(t, storedB)
		assert.Equal(t, 4, jobRecurringCount(t, storedB.Details))
		assert.Equal(t, 2, jobRecurringCount(t, first.Details))
	}
}

func testTRTPayloadStreamsEvaluation(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)
		t.Cleanup(func() { deleteWorkspaceItems(t, client) })

		now := time.Now().UTC()
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-nightly-accepted", trtStreamNightly, "Accepted", now.Add(-time.Hour), "", payloadDetails(t, "https://example.com/nightly", nil)))

		view := getTeamSLO(t, client)
		nightly := groupByKey(t, view.Evaluations[0], trtStreamNightly)
		ci := groupByKey(t, view.Evaluations[0], trtStreamCI)
		assert.Equal(t, 1, nightly.Accepted)
		assert.True(t, nightly.Met)
		require.NotNil(t, nightly.LastAcceptedAt)
		assert.False(t, ci.Met)
		assert.False(t, view.Evaluations[0].Met)

		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-ci-accepted", trtStreamCI, "Accepted", now.Add(-30*time.Minute), "", payloadDetails(t, "https://example.com/ci", nil)))
		view = getTeamSLO(t, client)
		assert.True(t, groupByKey(t, view.Evaluations[0], trtStreamCI).Met)
		assert.True(t, view.Evaluations[0].Met)
	}
}

func testTRTPayloadStreamsReplaceAndLink(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)
		t.Cleanup(func() { deleteWorkspaceItems(t, client) })

		now := time.Now().UTC()
		count := 4
		created := putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-b", trtStreamNightly, "Rejected", now.Add(-2*time.Hour), "second", payloadDetails(t, "https://example.com/b", &count)))
		replaced := putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-b", trtStreamNightly, "Rejected", now.Add(-2*time.Hour), "rewritten", payloadDetails(t, "https://example.com/b", &count)))
		assert.Equal(t, created.ID, replaced.ID)
		assert.Equal(t, "rewritten", replaced.Notes)
		assert.Equal(t, "chai-bot", replaced.UpdatedBy)
		assert.Equal(t, 4, jobRecurringCount(t, replaced.Details))

		link := putSLOLinkChai(t, client, "e2e-b", map[string]string{
			"url":       "https://redhat.atlassian.net/browse/TRT-1",
			"link_type": "jira",
		})
		again := putSLOLinkChai(t, client, "e2e-b", map[string]string{
			"url":       "https://redhat.atlassian.net/browse/TRT-1",
			"link_type": "jira",
		})
		assert.Equal(t, link.ID, again.ID)

		replaced = putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-b", trtStreamNightly, "Rejected", now.Add(-2*time.Hour), "rewritten again", payloadDetails(t, "https://example.com/b", &count)))
		require.Len(t, replaced.Links, 1)
		assert.Equal(t, link.ID, replaced.Links[0].ID)

		deleteSLOLink(t, client, "e2e-b", link.ID)
		resp, err := client.Delete(sloItemLinkPath("e2e-b", link.ID))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	}
}

func testTRTPayloadStreamsPrune(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		deleteWorkspaceItems(t, client)
		t.Cleanup(func() { deleteWorkspaceItems(t, client) })

		now := time.Now().UTC()
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-nightly-accepted", trtStreamNightly, "Accepted", now.Add(-time.Hour), "", payloadDetails(t, "https://example.com/nightly", nil)))
		// recent_payloads is 2, so two newer in-window rows have to exist before an
		// out-of-window row can leave the last-N set and be pruned.
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-old-rejected", trtStreamCI, "Rejected", now.Add(-72*time.Hour), "", payloadDetails(t, "https://example.com/old-r", nil)))
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-old-accepted", trtStreamCI, "Accepted", now.Add(-48*time.Hour), "", payloadDetails(t, "https://example.com/old-a", nil)))
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-ci-pad-1", trtStreamCI, "Rejected", now.Add(-4*time.Hour), "", payloadDetails(t, "https://example.com/pad-1", nil)))
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-ci-pad-2", trtStreamCI, "Rejected", now.Add(-3*time.Hour), "", payloadDetails(t, "https://example.com/pad-2", nil)))

		require.Contains(t, itemKeys(getTeamSLO(t, client).Items), "e2e-old-rejected")
		waitForItem(t, client, "e2e-old-rejected", false)
		held := getTeamSLO(t, client)
		assert.Contains(t, itemKeys(held.Items), "e2e-old-accepted")
		assert.NotContains(t, itemKeys(held.Items), "e2e-old-rejected")
		ci := groupByKey(t, held.Evaluations[0], trtStreamCI)
		assert.Equal(t, 0, ci.Accepted)
		assert.False(t, ci.Met)
		require.NotNil(t, ci.LastAcceptedAt)
		assert.False(t, held.Evaluations[0].Met)

		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-ci-new", trtStreamCI, "Accepted", now.Add(-30*time.Minute), "", payloadDetails(t, "https://example.com/ci", nil)))
		waitForItem(t, client, "e2e-old-accepted", false)
		released := getTeamSLO(t, client)
		assert.NotContains(t, itemKeys(released.Items), "e2e-old-accepted")
		assert.NotContains(t, itemKeys(released.Items), "e2e-old-rejected")
		assert.Contains(t, itemKeys(released.Items), "e2e-ci-new")
		assert.Contains(t, itemKeys(released.Items), "e2e-ci-pad-1")
		ci = groupByKey(t, released.Evaluations[0], trtStreamCI)
		assert.Equal(t, 1, ci.Accepted)
		assert.True(t, ci.Met)
		assert.True(t, released.Evaluations[0].Met)
	}
}

func testSLODeveloperUpsert(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		item := putSLOItemDeveloper(t, client, mustSLOItemBody(t, "e2e-developer", trtStreamNightly, "Accepted", time.Now().UTC(), "from the ui", payloadDetails(t, "https://example.com/dev", nil)))
		assert.Equal(t, "developer", item.UpdatedBy)
		deleteSLOItem(t, client, item.ItemKey)
	}
}

func testSLOComponentOutage(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		outage := createOutage(t, client, trtIncidentsComponent, trtIncidentsSub)
		defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, outage.ID)

		view := getTeamSLO(t, client)
		require.NotEmpty(t, view.SLOComponents)
		found := false
		for _, block := range view.SLOComponents {
			if block.Component != trtIncidentsComponent || block.SubComponent != trtIncidentsSub {
				continue
			}
			for _, active := range block.Outages {
				if active.ID == outage.ID {
					found = true
				}
			}
		}
		assert.True(t, found, "active TRT Incidents outage should appear on the team SLO")

		summary := getSLOSummary(t, client)
		require.Len(t, summary.Teams, 1)
		found = false
		for _, block := range summary.Teams[0].SLOComponents {
			for _, active := range block.Outages {
				if active.ID == outage.ID {
					found = true
				}
			}
		}
		assert.True(t, found, "active TRT Incidents outage should appear on the home SLO summary")
	}
}

func testSLOMissDoesNotCreateOutage(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		before := outageIDs(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
		putSLOItemChai(t, client, mustSLOItemBody(t, "e2e-miss", trtStreamNightly, "Rejected", time.Now().UTC(), "", payloadDetails(t, "https://example.com/miss", nil)))
		t.Cleanup(func() { deleteSLOItem(t, client, "e2e-miss") })

		after := getOutages(t, client, trtIncidentsComponent, trtIncidentsSub)
		assert.ElementsMatch(t, before, outageIDs(after))
	}
}

func assertSeededWorkspace(t *testing.T, view e2eTeamView) {
	t.Helper()
	assert.Equal(t, sloTeam, view.Team)
	require.NotNil(t, view.Workspace)
	streams := workspaceSettings(t, view.Workspace).Streams
	require.NotEmpty(t, streams)
	require.NotEmpty(t, view.Evaluations)
	ev := view.Evaluations[0]
	result := payloadResult(t, ev)
	require.Len(t, result.Groups, len(streams))
	require.NotEmpty(t, view.Items)
	if seed.MissIndex(len(streams)) >= 0 {
		assert.False(t, ev.Met)
	} else {
		assert.True(t, ev.Met)
	}

	for i, stream := range streams {
		group := groupByKey(t, ev, stream.Name)
		rows := itemsForStream(view.Items, stream.Name)
		require.NotEmpty(t, rows, stream.Name)
		assert.Equal(t, seed.UpdatedBy, rows[0].UpdatedBy)
		var details payloadv1.PayloadDetails
		require.NoError(t, json.Unmarshal(rows[0].Details, &details))
		assert.Contains(t, details.PayloadURL, stream.Name)
		switch seed.RoleAt(len(streams), i) {
		case seed.RoleMiss:
			assert.False(t, group.Met)
			assert.Equal(t, 0, group.Accepted)
			require.NotNil(t, group.LastAcceptedAt)
			assert.Equal(t, "Rejected", rows[0].Outcome)
		case seed.RoleRecentReject:
			assert.True(t, group.Met)
			assert.GreaterOrEqual(t, group.Accepted, 1)
			assert.Equal(t, "Rejected", rows[0].Outcome)
		default:
			assert.True(t, group.Met)
			assert.GreaterOrEqual(t, group.Accepted, 1)
			assert.Equal(t, "Accepted", rows[0].Outcome)
		}
	}

	outage := seedOutageIn(view.SLOComponents)
	require.NotNil(t, outage, "seeded TRT incident should be on the team SLO")
	assert.Equal(t, seed.OutageDescription, outage.Description)
	assert.Equal(t, types.SeverityDegraded, outage.Severity)

	for n, idx := range seed.JiraStreamIndexes(len(streams)) {
		row := newestRejectedView(view.Items, streams[idx].Name)
		require.NotNil(t, row, streams[idx].Name)
		assert.True(t, hasLink(row.Links, "jira", seed.JiraURL), streams[idx].Name)
		if n == 0 {
			assert.True(t, hasOutageLink(row.Links, outage.ID), streams[idx].Name)
		}
	}
}

func assertEvaluationsMatch(t *testing.T, team, summary []e2eEvaluation) {
	t.Helper()
	require.Len(t, summary, len(team))
	for i := range team {
		assert.Equal(t, team[i].Name, summary[i].Name)
		assert.Equal(t, team[i].Met, summary[i].Met)
		teamResult := payloadResult(t, team[i])
		summaryResult := payloadResult(t, summary[i])
		require.Len(t, summaryResult.Groups, len(teamResult.Groups))
		for j := range teamResult.Groups {
			assert.Equal(t, teamResult.Groups[j].Key, summaryResult.Groups[j].Key)
			assert.Equal(t, teamResult.Groups[j].Met, summaryResult.Groups[j].Met)
			assert.Equal(t, teamResult.Groups[j].Accepted, summaryResult.Groups[j].Accepted)
		}
	}
}

func seedOutageIn(blocks []e2eComponent) *types.Outage {
	for i := range blocks {
		for j := range blocks[i].Outages {
			if blocks[i].Outages[j].CreatedBy == seed.UpdatedBy {
				return &blocks[i].Outages[j]
			}
		}
	}
	return nil
}

func newestRejectedView(items []e2eItem, stream string) *e2eItem {
	var found *e2eItem
	for i := range items {
		item := &items[i]
		if item.GroupKey != stream || item.Outcome != "Rejected" {
			continue
		}
		if found == nil || item.OccurredAt.After(found.OccurredAt) {
			found = item
		}
	}
	return found
}

func hasLink(links []types.SLOWorkspaceLink, linkType, url string) bool {
	for _, link := range links {
		if link.LinkType == linkType && link.URL == url {
			return true
		}
	}
	return false
}

func hasOutageLink(links []types.SLOWorkspaceLink, outageID uint) bool {
	for _, link := range links {
		if link.LinkType == "outage" && link.OutageID != nil && *link.OutageID == outageID {
			return true
		}
	}
	return false
}

func getTeamSLO(t *testing.T, client *TestHTTPClient) e2eTeamView {
	t.Helper()
	return getTeamSLOPath(t, client, "/api/teams/"+sloTeam+"/slo")
}

func getTeamSLOPath(t *testing.T, client *TestHTTPClient, path string) e2eTeamView {
	t.Helper()
	resp, err := client.Get(path, false)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var view e2eTeamView
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&view))
	return view
}

func getSLOSummary(t *testing.T, client *TestHTTPClient) e2eSummary {
	t.Helper()
	resp, err := client.Get("/api/teams/slo-summary", false)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var summary e2eSummary
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&summary))
	return summary
}

func summaryTeamKeys(t *testing.T, client *TestHTTPClient) map[string]json.RawMessage {
	t.Helper()
	resp, err := client.Get("/api/teams/slo-summary", false)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var body struct {
		Teams []map[string]json.RawMessage `json:"teams"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Teams)
	return body.Teams[0]
}

func putSLOItemChai(t *testing.T, client *TestHTTPClient, body []byte) e2eItem {
	t.Helper()
	resp, err := client.PutWithBearerToken(sloItemsPath, body, chaiBotSAToken, "chai-bot")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var item e2eItem
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&item))
	return item
}

func putSLOItemDeveloper(t *testing.T, client *TestHTTPClient, body []byte) e2eItem {
	t.Helper()
	resp, err := client.Put(sloItemsPath, body)
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var item e2eItem
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&item))
	return item
}

func putSLOLinkChai(t *testing.T, client *TestHTTPClient, itemKey string, payload map[string]string) types.SLOWorkspaceLink {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	resp, err := client.PutWithBearerToken(sloItemPath(itemKey)+"/links", body, chaiBotSAToken, "chai-bot")
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
	var link types.SLOWorkspaceLink
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&link))
	return link
}

func deleteSLOItem(t *testing.T, client *TestHTTPClient, itemKey string) {
	t.Helper()
	resp, err := client.Delete(sloItemPath(itemKey))
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

func deleteSLOLink(t *testing.T, client *TestHTTPClient, itemKey string, linkID uint) {
	t.Helper()
	resp, err := client.Delete(sloItemLinkPath(itemKey, linkID))
	require.NoError(t, err)
	defer resp.Body.Close()
	requireStatus(t, resp, http.StatusOK)
}

func deleteWorkspaceItems(t *testing.T, client *TestHTTPClient) {
	t.Helper()
	view := getTeamSLO(t, client)
	for _, key := range itemKeys(view.Items) {
		deleteSLOItem(t, client, key)
	}
}

func sloItemPath(itemKey string) string {
	return sloItemsPath + "/" + trtPayloadKind + "/" + url.PathEscape(itemKey)
}

func sloItemLinkPath(itemKey string, linkID uint) string {
	return sloItemPath(itemKey) + "/links/" + strconvFormatUint(linkID)
}

func mustSLOItemBody(t *testing.T, itemKey, groupKey, outcome string, occurred time.Time, notes string, details json.RawMessage) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"kind":           trtPayloadKind,
		"schema_version": 1,
		"item_key":       itemKey,
		"group_key":      groupKey,
		"occurred_at":    occurred,
		"outcome":        outcome,
		"details":        details,
		"notes":          notes,
	})
	require.NoError(t, err)
	return body
}

func payloadDetails(t *testing.T, payloadURL string, recurring *int) json.RawMessage {
	t.Helper()
	doc := payloadv1.PayloadDetails{
		PayloadURL: payloadURL,
		Jobs: []payloadv1.PayloadJob{{
			Name:           "e2e-job",
			URL:            payloadURL + "/job",
			State:          "failure",
			RecurringCount: recurring,
		}},
	}
	body, err := json.Marshal(doc)
	require.NoError(t, err)
	return body
}

func jobRecurringCount(t *testing.T, details json.RawMessage) int {
	t.Helper()
	var doc payloadv1.PayloadDetails
	require.NoError(t, json.Unmarshal(details, &doc))
	require.NotEmpty(t, doc.Jobs)
	require.NotNil(t, doc.Jobs[0].RecurringCount)
	return *doc.Jobs[0].RecurringCount
}

func itemKeys(items []e2eItem) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.ItemKey)
	}
	return keys
}

func itemsForStream(items []e2eItem, stream string) []e2eItem {
	var out []e2eItem
	for _, item := range items {
		if item.GroupKey == stream {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].OccurredAt.After(out[j].OccurredAt)
	})
	return out
}

func itemByKey(t *testing.T, items []e2eItem, key string) *e2eItem {
	t.Helper()
	for i := range items {
		if items[i].ItemKey == key {
			return &items[i]
		}
	}
	return nil
}

func groupByKey(t *testing.T, ev e2eEvaluation, key string) e2eGroup {
	t.Helper()
	for _, group := range payloadResult(t, ev).Groups {
		if group.Key == key {
			return group
		}
	}
	t.Fatalf("missing evaluation group %s", key)
	return e2eGroup{}
}

func payloadResult(t *testing.T, ev e2eEvaluation) e2eResult {
	t.Helper()
	var result e2eResult
	require.NoError(t, json.Unmarshal(ev.Result, &result))
	return result
}

func workspaceSettings(t *testing.T, ws *e2eWorkspace) payloadv1.Settings {
	t.Helper()
	settings, err := payloadv1.ParseSettings(ws.Spec)
	require.NoError(t, err)
	return settings
}

func containsKey(keys []string, key string) bool {
	for _, item := range keys {
		if item == key {
			return true
		}
	}
	return false
}

func waitForItem(t *testing.T, client *TestHTTPClient, key string, present bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 45*time.Second, true, func(context.Context) (bool, error) {
		return containsKey(itemKeys(getTeamSLO(t, client).Items), key) == present, nil
	})
	require.NoError(t, err)
}

func outageIDs(outages []types.Outage) []uint {
	ids := make([]uint, 0, len(outages))
	for _, outage := range outages {
		ids = append(ids, outage.ID)
	}
	return ids
}

func requireStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode == want {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	t.Fatalf("status %d, want %d: %s", resp.StatusCode, want, body)
}

func strconvFormatUint(id uint) string {
	return jsonNumber(id)
}

func jsonNumber(id uint) string {
	body, _ := json.Marshal(id)
	return string(body)
}
