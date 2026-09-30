//nolint:errcheck,unparam // Test helpers - error handling and unused parameters are acceptable in test code
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	outage_pkg "ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/types"
	"ship-status-dash/pkg/utils"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	prowComponentName       = "Prow"
	componentMonitorSAToken = "component-monitor-sa-token"
	chaiBotSAToken          = "chai-bot-sa-token"
	mcpServerSAToken        = "mcp-server-sa-token"
)

func TestE2E_Dashboard(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping e2e test in short mode")
	}

	serverURL := os.Getenv("TEST_SERVER_URL")
	if serverURL == "" {
		t.Fatalf("TEST_SERVER_URL is not set")
	}
	mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
	if mockOauthProxyURL == "" {
		t.Fatalf("TEST_MOCK_OAUTH_PROXY_URL is not set")
	}
	client, err := NewTestHTTPClient(serverURL, mockOauthProxyURL)
	require.NoError(t, err)

	cleanupAbsentReportOutages(t, client)

	t.Run("Health", testHealth(client))
	t.Run("Components", testComponents(client))
	t.Run("ComponentInfo", testComponentInfo(client))
	t.Run("Outages", testOutages(client))
	t.Run("OutagesDuring", testOutagesDuring(client))
	t.Run("UpdateOutage", testUpdateOutage(client))
	t.Run("DeleteOutage", testDeleteOutage(client))
	t.Run("GetOutage", testGetOutage(client))
	t.Run("OutageAuditLogs", testOutageAuditLogs(client))
	t.Run("SubComponentStatus", testSubComponentStatus(client))
	t.Run("ComponentStatus", testComponentStatus(client))
	t.Run("AllComponentsStatus", testAllComponentsStatus(client))
	t.Run("ListSubComponents", testListSubComponents(client))
	t.Run("Tags", testTags(client))
	t.Run("User", testUser(client))
	t.Run("TriageNotes", testTriageNotes(client))
	t.Run("OutageLinks", testOutageLinks(client))
	t.Run("OutageRelationships", testOutageRelationships(client))
	t.Run("ServiceAccountOutages", testServiceAccountOutages(client))
	t.Run("DelegatedAuthorization", testDelegatedAuthorization(client))
	t.Run("AbsentReport", testAbsentReport(client))
	t.Run("CommunityReport", testCommunityReport(client))
	// ConfigHotReload should be tested at the end, it attempts to clean up after itself, but due to the nature of timing,
	// and inspecting pod logs in ci, it is not guaranteed to do so successfully.
	t.Run("ConfigHotReload", testConfigHotReload(client))
}

func testHealth(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		resp, err := client.Get("/health", false)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		var health map[string]interface{}
		err = json.NewDecoder(resp.Body).Decode(&health)
		require.NoError(t, err)

		assert.Equal(t, "ok", health["status"])
		assert.NotEmpty(t, health["time"])
	}
}

func testComponents(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		components := getComponents(t, client)

		assert.Len(t, components, 6)
		assert.Equal(t, "Prow", components[0].Name)
		assert.Equal(t, "Backbone of the CI system", components[0].Description)
		assert.Equal(t, "TestPlatform", components[0].ShipTeam)
		assert.Len(t, components[0].SlackReporting, 1)
		assert.Equal(t, "#test-channel", components[0].SlackReporting[0].Channel)
		assert.Equal(t, types.SeverityDown, *components[0].SlackReporting[0].Severity)
		assert.Len(t, components[0].Subcomponents, 4)
		assert.Equal(t, "Tide", components[0].Subcomponents[0].Name)
		assert.Equal(t, "Deck", components[0].Subcomponents[1].Name)
		assert.Equal(t, "Hook", components[0].Subcomponents[2].Name)
		assert.Equal(t, "Plank", components[0].Subcomponents[3].Name)

		assert.Equal(t, "Downstream CI", components[1].Name)
		assert.Equal(t, "Downstream CI system", components[1].Description)
		assert.Equal(t, "TestPlatform", components[1].ShipTeam)
		assert.Len(t, components[1].SlackReporting, 1)
		assert.Equal(t, "#test-channel", components[1].SlackReporting[0].Channel)
		assert.Equal(t, types.SeverityDown, *components[1].SlackReporting[0].Severity)
		assert.Len(t, components[1].Subcomponents, 1)
		assert.Equal(t, "Retester", components[1].Subcomponents[0].Name)

		assert.Equal(t, "Build Farm", components[2].Name)
		assert.Equal(t, "Where the CI jobs are run", components[2].Description)
		assert.Equal(t, "DPTP", components[2].ShipTeam)
		assert.Len(t, components[2].SlackReporting, 1)
		assert.Equal(t, "#ops-testplatform", components[2].SlackReporting[0].Channel)
		assert.Equal(t, types.SeverityDown, *components[2].SlackReporting[0].Severity)
		assert.Len(t, components[2].Subcomponents, 2)
		assert.Equal(t, "Build01", components[2].Subcomponents[0].Name)
		assert.Equal(t, "Build02", components[2].Subcomponents[1].Name)

		assert.Equal(t, "Boskos", components[3].Name)
		assert.Equal(t, "Resource leasing for CI workloads", components[3].Description)
		assert.Equal(t, "DPTP", components[3].ShipTeam)
		assert.Len(t, components[3].SlackReporting, 1)
		assert.Equal(t, "#ops-testplatform", components[3].SlackReporting[0].Channel)
		assert.Len(t, components[3].Subcomponents, 2)
		assert.Equal(t, "Quota", components[3].Subcomponents[0].Name)
		assert.Equal(t, "Leases", components[3].Subcomponents[1].Name)

		assert.Equal(t, "Sippy", components[4].Name)
		assert.Equal(t, "CI private investigator", components[4].Description)
		assert.Equal(t, "TRT", components[4].ShipTeam)
		assert.Len(t, components[4].SlackReporting, 1)
		assert.Equal(t, "#trt-alert", components[4].SlackReporting[0].Channel)
		assert.Equal(t, types.SeverityDown, *components[4].SlackReporting[0].Severity)
		assert.Len(t, components[4].Subcomponents, 5)
		assert.Equal(t, "Sippy", components[4].Subcomponents[0].Name)
		assert.Equal(t, "api", components[4].Subcomponents[1].Name)
		assert.Equal(t, "data-load", components[4].Subcomponents[2].Name)

		assert.Equal(t, "Errata Reliability", components[5].Name)
		assert.Equal(t, "Services maintained by the Errata Reliability Team", components[5].Description)
		assert.Equal(t, "ERT", components[5].ShipTeam)
		assert.Len(t, components[5].Subcomponents, 1)
		assert.Equal(t, "systemd-test", components[5].Subcomponents[0].Name)
	}
}

func testComponentInfo(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET component info for existing component returns component details", func(t *testing.T) {
			component := getComponent(t, client, "Prow")

			assert.Equal(t, "Prow", component.Name)
			assert.Equal(t, "Backbone of the CI system", component.Description)
			assert.Equal(t, "TestPlatform", component.ShipTeam)
			assert.Len(t, component.SlackReporting, 1)
			assert.Equal(t, "#test-channel", component.SlackReporting[0].Channel)
			assert.Equal(t, types.SeverityDown, *component.SlackReporting[0].Severity)
			assert.Len(t, component.Subcomponents, 4)
			assert.Equal(t, "Tide", component.Subcomponents[0].Name)
			assert.Equal(t, "Deck", component.Subcomponents[1].Name)
			assert.Equal(t, "Hook", component.Subcomponents[2].Name)
			assert.Equal(t, "Plank", component.Subcomponents[3].Name)
		})

		t.Run("GET component info for non-existent component returns 404", func(t *testing.T) {
			expect404(t, client, "/api/components/"+utils.Slugify("NonExistentComponent"), false)
		})
	}
}

// createOutage is a helper function to create an outage for testing
func createOutage(t *testing.T, client *TestHTTPClient, componentName, subComponentName string) types.Outage {
	outagePayload := map[string]interface{}{
		"severity":        string(types.SeverityDown),
		"start_time":      time.Now().UTC().Format(time.RFC3339),
		"description":     "Test outage for " + subComponentName,
		"discovered_from": "e2e-test",
		"created_by":      "developer",
	}

	payloadBytes, err := json.Marshal(outagePayload)
	require.NoError(t, err)

	resp, err := client.Post(fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify(componentName), utils.Slugify(subComponentName)), payloadBytes)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var outage types.Outage
	err = json.NewDecoder(resp.Body).Decode(&outage)
	require.NoError(t, err)

	// Verify that created_by is set to the user from X-Forwarded-User header
	assert.Equal(t, "developer", outage.CreatedBy, "created_by should be set to the user from X-Forwarded-User header")

	return outage
}

// deleteOutage is a helper function to delete an outage for cleanup
func deleteOutage(t *testing.T, client *TestHTTPClient, componentName, subComponentName string, outageID uint) {
	resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify(componentName), utils.Slugify(subComponentName), outageID))
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

// updateOutage is a helper function to update an outage
func updateOutage(t *testing.T, client *TestHTTPClient, componentName, subComponentName string, outageID uint, payload map[string]interface{}) {
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	updateURL := fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify(componentName), utils.Slugify(subComponentName), outageID)
	resp, err := client.Patch(updateURL, payloadBytes)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func testOutagesDuring(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		o := createOutage(t, client, "Prow", "Deck")
		defer deleteOutage(t, client, "Prow", "Deck", o.ID)

		st := o.StartTime.UTC()
		mid := st.Add(30 * time.Second).Format(time.RFC3339Nano)
		found := getOutagesDuring(t, client, mid, "", utils.Slugify("Prow"), utils.Slugify("Deck"), "", "")
		var saw bool
		for _, row := range found {
			if row.ID == o.ID {
				saw = true
				break
			}
		}
		require.True(t, saw, "expected outage at instant mid")

		winStart := st.Add(-time.Minute).Format(time.RFC3339)
		winEnd := st.Add(time.Hour).Format(time.RFC3339)
		found2 := getOutagesDuring(t, client, winStart, winEnd, "", "", "", "")
		saw = false
		for _, row := range found2 {
			if row.ID == o.ID {
				saw = true
				break
			}
		}
		require.True(t, saw, "expected outage in overlapping range")

		earlyStart := st.Add(-72 * time.Hour).Format(time.RFC3339)
		earlyEnd := st.Add(-48 * time.Hour).Format(time.RFC3339)
		empty := getOutagesDuring(t, client, earlyStart, earlyEnd, "", "", "", "")
		for _, row := range empty {
			assert.NotEqualf(t, o.ID, row.ID, "outage %d should not appear in non-overlapping window", o.ID)
		}

		resp, err := client.Get("/api/outages/during", false)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

		t.Run("tag_and_team_filters", func(t *testing.T) {
			o := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", o.ID)

			st := o.StartTime.UTC()
			mid := st.Add(30 * time.Second).Format(time.RFC3339Nano)
			found := getOutagesDuring(t, client, mid, "", "", "", "pr-merging", "TestPlatform")
			var saw bool
			for _, row := range found {
				if row.ID == o.ID {
					saw = true
					break
				}
			}
			require.True(t, saw, "expected outage when filtering by Prow ship_team (TestPlatform) and Tide tag (pr-merging)")
		})
	}
}

func testOutages(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("POST to sub-component succeeds", func(t *testing.T) {
			outage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", outage.ID)

			assert.NotZero(t, outage.ID)
			assert.Equal(t, utils.Slugify("Prow"), outage.ComponentName)
			assert.Equal(t, utils.Slugify("Tide"), outage.SubComponentName)
			assert.Equal(t, string(types.SeverityDown), string(outage.Severity))
			assert.Equal(t, "e2e-test", outage.DiscoveredFrom)
		})

		t.Run("POST to non-existent sub-component fails", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Test outage for non-existent sub-component",
				"discovered_from": "e2e-test",
				"created_by":      "developer",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.Post(fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("NonExistentSub")), payloadBytes)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("POST with invalid severity fails", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        "InvalidSeverity",
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Test outage with invalid severity",
				"discovered_from": "e2e-test",
				"created_by":      "developer",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.Post(fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Deck")), payloadBytes)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Contains(t, errorResponse["error"], "Invalid severity")
		})

		t.Run("GET on top-level component aggregates sub-components", func(t *testing.T) {
			// Create outages for different sub-components
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)
			deckOutage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)

			outages := getOutages(t, client, "Prow", "")

			// Should have exactly our 2 outages since we clean up after ourselves
			assert.Len(t, outages, 2)

			// Verify our specific outages are present
			outageIDs := make(map[uint]bool)
			for _, outage := range outages {
				outageIDs[outage.ID] = true
			}
			assert.True(t, outageIDs[tideOutage.ID], "Tide outage should be present")
			assert.True(t, outageIDs[deckOutage.ID], "Deck outage should be present")
		})

		t.Run("GET on sub-component returns only that sub-component's outages", func(t *testing.T) {
			// Create outages for different sub-components
			tideOutage1 := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage1.ID)
			tideOutage2 := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage2.ID)
			deckOutage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)

			outages := getOutages(t, client, "Prow", "Tide")

			// Should have exactly our 2 Tide outages since we clean up after ourselves
			assert.Len(t, outages, 2)

			// All outages should be for Tide only
			for _, outage := range outages {
				assert.Equal(t, utils.Slugify("Tide"), outage.SubComponentName)
			}

			// Verify our specific outages are present
			outageIDs := make(map[uint]bool)
			for _, outage := range outages {
				outageIDs[outage.ID] = true
			}
			assert.True(t, outageIDs[tideOutage1.ID], "First Tide outage should be present")
			assert.True(t, outageIDs[tideOutage2.ID], "Second Tide outage should be present")
			assert.False(t, outageIDs[deckOutage.ID], "Deck outage should not be included")
		})

		t.Run("GET on non-existent sub-component fails", func(t *testing.T) {
			// This test doesn't need any setup - it should fail regardless of existing data
			expect404(t, client, fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("NonExistentSub")), false)
		})

		t.Run("POST to unauthorized component returns 403", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Test outage for unauthorized component",
				"discovered_from": "e2e-test",
				"created_by":      "developer",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			expect403(t, client, "POST", fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Build Farm"), utils.Slugify("Build01")), payloadBytes)
		})
	}
}

func testUpdateOutage(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		// Create an outage to update
		createdOutage := createOutage(t, client, "Prow", "Tide")
		defer deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

		// Verify that StartTime is rounded to the nearest second (no sub-second precision)
		assert.Equal(t, 0, createdOutage.StartTime.Nanosecond(), "StartTime should be rounded to the nearest second")

		// Now update the outage
		updatePayload := map[string]interface{}{
			"severity":    string(types.SeverityDegraded),
			"description": "Updated description",
		}

		updateBytes, err := json.Marshal(updatePayload)
		require.NoError(t, err)

		updateURL := fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID)
		t.Logf("Making PATCH request to: %s", updateURL)

		updateResp, err := client.Patch(updateURL, updateBytes)
		require.NoError(t, err)
		defer updateResp.Body.Close()

		if updateResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(updateResp.Body)
			t.Logf("Unexpected status %d, body: %s", updateResp.StatusCode, string(body))
		}

		assert.Equal(t, http.StatusOK, updateResp.StatusCode)
		assert.Equal(t, "application/json", updateResp.Header.Get("Content-Type"))

		var updatedOutage types.Outage
		err = json.NewDecoder(updateResp.Body).Decode(&updatedOutage)
		require.NoError(t, err)

		assert.Equal(t, createdOutage.ID, updatedOutage.ID)
		assert.Equal(t, string(types.SeverityDegraded), string(updatedOutage.Severity))
		assert.Equal(t, "Updated description", updatedOutage.Description)
		assert.WithinDuration(t, createdOutage.StartTime.UTC(), updatedOutage.StartTime.UTC(), time.Second) // Should remain unchanged
		assert.Equal(t, createdOutage.CreatedBy, updatedOutage.CreatedBy)                                   // Should remain unchanged

		// Test updating non-existent outage
		nonExistentResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/99999", utils.Slugify("Prow"), utils.Slugify("Tide")), updateBytes)
		require.NoError(t, err)
		defer nonExistentResp.Body.Close()

		assert.Equal(t, http.StatusNotFound, nonExistentResp.StatusCode)

		// Test updating with invalid component
		invalidComponentResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("NonExistentComponent"), utils.Slugify("Tide"), createdOutage.ID), updateBytes)
		require.NoError(t, err)
		defer invalidComponentResp.Body.Close()

		assert.Equal(t, http.StatusNotFound, invalidComponentResp.StatusCode)

		// Test updating with invalid severity
		invalidSeverityUpdate := map[string]interface{}{
			"severity": "InvalidSeverity",
		}
		invalidSeverityBytes, err := json.Marshal(invalidSeverityUpdate)
		require.NoError(t, err)

		invalidSeverityResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), invalidSeverityBytes)
		require.NoError(t, err)
		defer invalidSeverityResp.Body.Close()

		assert.Equal(t, http.StatusBadRequest, invalidSeverityResp.StatusCode)

		var errorResponse map[string]string
		err = json.NewDecoder(invalidSeverityResp.Body).Decode(&errorResponse)
		require.NoError(t, err)
		assert.Contains(t, errorResponse["error"], "Invalid severity")

		// Test confirming an outage
		confirmPayload := map[string]interface{}{
			"confirmed": true,
		}
		confirmBytes, err := json.Marshal(confirmPayload)
		require.NoError(t, err)

		confirmResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), confirmBytes)
		require.NoError(t, err)
		defer confirmResp.Body.Close()

		assert.Equal(t, http.StatusOK, confirmResp.StatusCode)

		var confirmedOutage types.Outage
		err = json.NewDecoder(confirmResp.Body).Decode(&confirmedOutage)
		require.NoError(t, err)

		assert.True(t, confirmedOutage.ConfirmedAt.Valid, "confirmed_at should be set when confirmed is true")
		// Verify that ConfirmedAt is rounded to the nearest second (no sub-second precision)
		assert.Equal(t, 0, confirmedOutage.ConfirmedAt.Time.Nanosecond(), "ConfirmedAt should be rounded to the nearest second")

		t.Run("PATCH to unauthorized component returns 403", func(t *testing.T) {
			updatePayload := map[string]interface{}{
				"severity": string(types.SeverityDegraded),
			}

			updateBytes, err := json.Marshal(updatePayload)
			require.NoError(t, err)

			expect403(t, client, "PATCH", fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("Build Farm"), utils.Slugify("Build01")), updateBytes)
		})

		t.Run("resolved_by should not change when end_time is unchanged, but should change when end_time is modified", func(t *testing.T) {
			serverURL := os.Getenv("TEST_SERVER_URL")
			require.NotEmpty(t, serverURL, "TEST_SERVER_URL must be set")
			mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
			require.NotEmpty(t, mockOauthProxyURL, "TEST_MOCK_OAUTH_PROXY_URL must be set")

			// Create an outage with user1 (developer)
			createdOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

			// Resolve the outage with user1 (developer) by setting end_time
			resolveTime := time.Now().UTC()
			resolvePayload := map[string]interface{}{
				"end_time": map[string]interface{}{
					"Time":  resolveTime.Format(time.RFC3339),
					"Valid": true,
				},
			}
			resolveBytes, err := json.Marshal(resolvePayload)
			require.NoError(t, err)

			resolveResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), resolveBytes)
			require.NoError(t, err)
			defer resolveResp.Body.Close()

			assert.Equal(t, http.StatusOK, resolveResp.StatusCode)

			var resolvedOutage types.Outage
			err = json.NewDecoder(resolveResp.Body).Decode(&resolvedOutage)
			require.NoError(t, err)

			assert.True(t, resolvedOutage.EndTime.Valid, "end_time should be valid after resolution")
			// Verify that EndTime is rounded to the nearest second (no sub-second precision)
			assert.Equal(t, 0, resolvedOutage.EndTime.Time.Nanosecond(), "EndTime should be rounded to the nearest second")
			originalEndTime := resolvedOutage.EndTime.Time

			// Now update the outage with user2 (editor) without changing end_time
			editorClient, err := NewTestHTTPClientWithUsername(serverURL, mockOauthProxyURL, "editor")
			require.NoError(t, err)

			updatePayload := map[string]interface{}{
				"description": "Updated description by editor",
			}
			updateBytes, err := json.Marshal(updatePayload)
			require.NoError(t, err)

			updateResp, err := editorClient.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), updateBytes)
			require.NoError(t, err)
			defer updateResp.Body.Close()

			assert.Equal(t, http.StatusOK, updateResp.StatusCode)

			var updatedOutage types.Outage
			err = json.NewDecoder(updateResp.Body).Decode(&updatedOutage)
			require.NoError(t, err)

			assert.True(t, updatedOutage.EndTime.Valid, "end_time should still be valid")
			assert.WithinDuration(t, originalEndTime, updatedOutage.EndTime.Time, time.Second, "end_time should not have changed")

			// Now update the outage with user2 (editor) by changing end_time
			newResolveTime := time.Now().UTC().Add(1 * time.Hour)
			changeEndTimePayload := map[string]interface{}{
				"end_time": map[string]interface{}{
					"Time":  newResolveTime.Format(time.RFC3339),
					"Valid": true,
				},
			}
			changeEndTimeBytes, err := json.Marshal(changeEndTimePayload)
			require.NoError(t, err)

			changeEndTimeResp, err := editorClient.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), changeEndTimeBytes)
			require.NoError(t, err)
			defer changeEndTimeResp.Body.Close()

			assert.Equal(t, http.StatusOK, changeEndTimeResp.StatusCode)

			var changedEndTimeOutage types.Outage
			err = json.NewDecoder(changeEndTimeResp.Body).Decode(&changedEndTimeOutage)
			require.NoError(t, err)

			assert.True(t, changedEndTimeOutage.EndTime.Valid, "end_time should still be valid")
			assert.WithinDuration(t, newResolveTime, changedEndTimeOutage.EndTime.Time, time.Second, "end_time should have been updated")
		})
	}
}

func testDeleteOutage(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("DELETE existing outage succeeds", func(t *testing.T) {
			// Create an outage to delete
			createdOutage := createOutage(t, client, "Prow", "Tide")

			// Delete the outage
			deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

			// Verify the outage is deleted by trying to get it
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var outages []types.Outage
			err = json.NewDecoder(resp.Body).Decode(&outages)
			require.NoError(t, err)

			// The deleted outage should not be in the list
			for _, outage := range outages {
				assert.NotEqual(t, createdOutage.ID, outage.ID, "Deleted outage should not be present")
			}
		})

		t.Run("DELETE non-existent outage returns 404", func(t *testing.T) {
			resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/99999", utils.Slugify("Prow"), utils.Slugify("Tide")))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("DELETE outage from non-existent component returns 404", func(t *testing.T) {
			resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("NonExistentComponent"), utils.Slugify("Tide")))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("DELETE outage from non-existent sub-component returns 404", func(t *testing.T) {
			resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("Prow"), utils.Slugify("NonExistentSub")))
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("DELETE outage from unauthorized component returns 403", func(t *testing.T) {
			expect403(t, client, "DELETE", fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("Build Farm"), utils.Slugify("Build01")), nil)
		})
	}
}

func testGetOutage(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET existing outage succeeds", func(t *testing.T) {
			// Create an outage to retrieve
			createdOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

			// Get the outage
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			var outage types.Outage
			err = json.NewDecoder(resp.Body).Decode(&outage)
			require.NoError(t, err)

			assert.Equal(t, createdOutage.ID, outage.ID)
			assert.Equal(t, utils.Slugify("Tide"), outage.SubComponentName)
			assert.Equal(t, string(types.SeverityDown), string(outage.Severity))
			assert.Equal(t, "e2e-test", outage.DiscoveredFrom)
			assert.Equal(t, "developer", outage.CreatedBy)
		})

		t.Run("GET non-existent outage returns 404", func(t *testing.T) {
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/99999", utils.Slugify("Prow"), utils.Slugify("Tide")), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("GET outage from non-existent component returns 404", func(t *testing.T) {
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("NonExistentComponent"), utils.Slugify("Tide")), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("GET outage from non-existent sub-component returns 404", func(t *testing.T) {
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/1", utils.Slugify("Prow"), utils.Slugify("NonExistentSub")), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})

		t.Run("GET outage with wrong sub-component returns 404", func(t *testing.T) {
			// Create an outage for Tide
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)

			// Try to get it as if it were a Deck outage
			resp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Deck"), tideOutage.ID), false)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

func testOutageAuditLogs(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET audit-logs after create, update, and delete", func(t *testing.T) {
			createdOutage := createOutage(t, client, "Prow", "Tide")

			auditLogsURL := fmt.Sprintf("/api/components/%s/%s/outages/%d/audit-logs", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID)

			resp, err := client.Get(auditLogsURL, false)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var logsAfterCreate []types.OutageAuditLog
			err = json.NewDecoder(resp.Body).Decode(&logsAfterCreate)
			require.NoError(t, err)
			require.Len(t, logsAfterCreate, 1, "audit logs after create should have one entry")
			assert.Equal(t, createdOutage.ID, logsAfterCreate[0].OutageID)
			assert.Equal(t, "CREATE", logsAfterCreate[0].Operation)
			assert.Equal(t, "developer", logsAfterCreate[0].User)

			updateOutage(t, client, "Prow", "Tide", createdOutage.ID, map[string]interface{}{
				"description": "Updated description for audit test",
			})

			resp2, err := client.Get(auditLogsURL, false)
			require.NoError(t, err)
			defer resp2.Body.Close()
			require.Equal(t, http.StatusOK, resp2.StatusCode)

			var logsAfterUpdate []types.OutageAuditLog
			err = json.NewDecoder(resp2.Body).Decode(&logsAfterUpdate)
			require.NoError(t, err)
			require.Len(t, logsAfterUpdate, 2, "audit logs after update should have two entries")
			// API returns logs newest first (created_at DESC)
			assert.Equal(t, "UPDATE", logsAfterUpdate[0].Operation)
			assert.Equal(t, "CREATE", logsAfterUpdate[1].Operation)
			assert.Equal(t, "developer", logsAfterUpdate[0].User)

			deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

			resp3, err := client.Get(auditLogsURL, false)
			require.NoError(t, err)
			defer resp3.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp3.StatusCode, "audit-logs for deleted outage should return 404")
		})

		t.Run("last_auditable_update tracks newest audit log CreatedAt", func(t *testing.T) {
			createdOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", createdOutage.ID)

			outageURL := fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), createdOutage.ID)
			auditLogsURL := fmt.Sprintf("%s/audit-logs", outageURL)
			notesURL := fmt.Sprintf("%s/triage-notes", outageURL)

			assertLastAuditableMatchesNewestAudit := func(t *testing.T) (previous time.Time) {
				t.Helper()

				resp, err := client.Get(outageURL, false)
				require.NoError(t, err)
				defer resp.Body.Close()
				require.Equal(t, http.StatusOK, resp.StatusCode)

				var outage types.Outage
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&outage))
				require.False(t, outage.LastAuditableUpdate.IsZero(), "last_auditable_update should be set")

				logsResp, err := client.Get(auditLogsURL, false)
				require.NoError(t, err)
				defer logsResp.Body.Close()
				require.Equal(t, http.StatusOK, logsResp.StatusCode)

				var logs []types.OutageAuditLog
				require.NoError(t, json.NewDecoder(logsResp.Body).Decode(&logs))
				require.NotEmpty(t, logs)
				assert.True(t, outage.LastAuditableUpdate.Equal(logs[0].CreatedAt),
					"last_auditable_update (%v) should equal newest audit CreatedAt (%v)",
					outage.LastAuditableUpdate, logs[0].CreatedAt)

				return outage.LastAuditableUpdate
			}

			afterCreate := assertLastAuditableMatchesNewestAudit(t)

			time.Sleep(5 * time.Millisecond)
			updateOutage(t, client, "Prow", "Tide", createdOutage.ID, map[string]interface{}{
				"description": "Updated for last_auditable_update test",
			})
			afterUpdate := assertLastAuditableMatchesNewestAudit(t)
			assert.True(t, afterUpdate.After(afterCreate), "last_auditable_update should advance on outage update")

			time.Sleep(5 * time.Millisecond)
			noteBody, err := json.Marshal(map[string]string{"body": "Note for last_auditable_update test"})
			require.NoError(t, err)
			noteResp, err := client.Post(notesURL, noteBody)
			require.NoError(t, err)
			defer noteResp.Body.Close()
			require.Equal(t, http.StatusCreated, noteResp.StatusCode)

			afterNote := assertLastAuditableMatchesNewestAudit(t)
			assert.True(t, afterNote.After(afterUpdate), "last_auditable_update should advance on triage note create")
		})
	}
}

func testSubComponentStatus(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET status for healthy sub-component returns Healthy", func(t *testing.T) {
			status := getStatus(t, client, "Prow", "Deck")

			assert.Equal(t, types.StatusHealthy, status.Status)
			assert.Empty(t, status.ActiveOutages)
		})

		t.Run("GET status for sub-component with active outage returns outage severity", func(t *testing.T) {
			// Create an outage for Deck (should be auto-confirmed)
			outage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", outage.ID)

			status := getStatus(t, client, "Prow", "Deck")

			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 1)
			assert.Equal(t, string(types.SeverityDown), string(status.ActiveOutages[0].Severity))
		})

		t.Run("GET status for sub-component with multiple outages returns most critical", func(t *testing.T) {
			// Create a Degraded outage for Tide
			degradedOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Tide", degradedOutage.ID)

			// Create a Down outage for Tide
			downOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Tide", downOutage.ID)

			// Confirm both outages to test most critical logic
			updateOutage(t, client, "Prow", "Tide", degradedOutage.ID, map[string]interface{}{
				"confirmed": true,
			})
			updateOutage(t, client, "Prow", "Tide", downOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			status := getStatus(t, client, "Prow", "Tide")

			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 2)
		})

		t.Run("GET status for non-existent component returns 404", func(t *testing.T) {
			expect404(t, client, fmt.Sprintf("/api/status/%s/%s", utils.Slugify("NonExistent"), utils.Slugify("Deck")), false)
		})

		t.Run("GET status for non-existent sub-component returns 404", func(t *testing.T) {
			expect404(t, client, fmt.Sprintf("/api/status/%s/%s", utils.Slugify("Prow"), utils.Slugify("NonExistent")), false)
		})

		t.Run("GET status for sub-component with future end_time still considers outage active", func(t *testing.T) {
			// Create an outage first (should be auto-confirmed)
			outage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", outage.ID)

			// Update the outage to have a future end_time
			futureTime := time.Now().Add(24 * time.Hour) // 24 hours in the future
			updatePayload := map[string]interface{}{
				"end_time": map[string]interface{}{
					"Time":  futureTime.UTC().Format(time.RFC3339),
					"Valid": true,
				},
			}

			updateBytes, err := json.Marshal(updatePayload)
			require.NoError(t, err)

			updateResp, err := client.Patch(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Deck"), outage.ID), updateBytes)
			require.NoError(t, err)
			defer updateResp.Body.Close()

			assert.Equal(t, http.StatusOK, updateResp.StatusCode)

			var updatedOutage types.Outage
			err = json.NewDecoder(updateResp.Body).Decode(&updatedOutage)
			require.NoError(t, err)

			// Check that the status endpoint still considers this outage active
			status := getStatus(t, client, "Prow", "Deck")

			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 1)
			assert.Equal(t, outage.ID, status.ActiveOutages[0].ID)
		})

		t.Run("GET status for sub-component with unconfirmed outage returns Suspected", func(t *testing.T) {
			// Create an unconfirmed outage for Tide (which has requires_confirmation: true)
			outage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", outage.ID)

			status := getStatus(t, client, "Prow", "Tide")

			assert.Equal(t, types.StatusSuspected, status.Status)
			assert.Len(t, status.ActiveOutages, 1)
			assert.False(t, status.ActiveOutages[0].ConfirmedAt.Valid)
		})

		t.Run("GET status for sub-component with mixed confirmed/unconfirmed outages returns confirmed severity", func(t *testing.T) {
			// Create confirmed degraded outage
			confirmedOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Tide", confirmedOutage.ID)

			// Confirm the degraded outage
			updateOutage(t, client, "Prow", "Tide", confirmedOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			// Create unconfirmed down outage
			unconfirmedOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Tide", unconfirmedOutage.ID)

			status := getStatus(t, client, "Prow", "Tide")

			// Should return Degraded (confirmed) not Suspected (unconfirmed)
			assert.Equal(t, types.StatusDegraded, status.Status)
			assert.Len(t, status.ActiveOutages, 2)
		})

		t.Run("GET status for sub-component without ping returns no last_ping_time", func(t *testing.T) {
			status := getStatus(t, client, "Prow", "Deck")

			assert.Equal(t, types.StatusHealthy, status.Status)
			assert.Nil(t, status.LastPingTime, "last_ping_time should be nil when no ping has been sent")
		})

		t.Run("GET status for sub-component with ping returns last_ping_time", func(t *testing.T) {
			// Send a component monitor report to create a ping
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusHealthy,
						Reasons:          []types.Reason{{Type: types.CheckTypePrometheus}},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			reportSentTime := time.Now()
			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			status := getStatus(t, client, "Prow", "Hook")

			assert.NotNil(t, status.LastPingTime, "last_ping_time should be set after component monitor report")
			assert.WithinDuration(t, reportSentTime, *status.LastPingTime, 5*time.Second, "last_ping_time should be within 5 seconds of when report was sent")
		})

		t.Run("GET status for sub-component updates last_ping_time on subsequent reports", func(t *testing.T) {
			// Send first report
			reportPayload1 := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusHealthy,
						Reasons:          []types.Reason{{Type: types.CheckTypePrometheus}},
					},
				},
			}

			payloadBytes1, err := json.Marshal(reportPayload1)
			require.NoError(t, err)

			resp1, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes1, componentMonitorSAToken)
			require.NoError(t, err)
			resp1.Body.Close()
			assert.Equal(t, http.StatusOK, resp1.StatusCode)

			status1 := getStatus(t, client, "Prow", "Hook")
			require.NotNil(t, status1.LastPingTime, "first report should set last_ping_time")
			firstPingTime := *status1.LastPingTime

			// Wait a moment to ensure timestamp is different
			time.Sleep(200 * time.Millisecond)

			// Send second report
			resp2, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes1, componentMonitorSAToken)
			require.NoError(t, err)
			resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			status2 := getStatus(t, client, "Prow", "Hook")
			require.NotNil(t, status2.LastPingTime, "second report should update last_ping_time")
			secondPingTime := *status2.LastPingTime

			assert.True(t, secondPingTime.After(firstPingTime), "second ping time should be after first ping time")
		})
	}
}

func testComponentStatus(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET status for healthy component returns Healthy", func(t *testing.T) {
			status := getStatus(t, client, "Prow", "")

			assert.Equal(t, types.StatusHealthy, status.Status)
			assert.Empty(t, status.ActiveOutages)
		})

		t.Run("GET status for component with one degraded sub-component returns Partial", func(t *testing.T) {
			// Create a degraded outage for Deck (doesn't require confirmation)
			deckOutage := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)

			status := getStatus(t, client, "Prow", "")

			assert.Equal(t, types.StatusPartial, status.Status)
			assert.Len(t, status.ActiveOutages, 1)
			assert.Equal(t, string(types.SeverityDegraded), string(status.ActiveOutages[0].Severity))
		})

		t.Run("GET status for component with all sub-components down returns Down", func(t *testing.T) {
			// Create Down outages for all sub-components
			tideOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)
			deckOutage := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)
			hookOutage := createOutageWithSeverity(t, client, "Prow", "Hook", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			plankOutage := createOutageWithSeverity(t, client, "Prow", "Plank", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)

			// Confirm Tide outage (others should be auto-confirmed)
			updateOutage(t, client, "Prow", "Tide", tideOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			status := getStatus(t, client, "Prow", "")

			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 4)
			for _, outage := range status.ActiveOutages {
				assert.Equal(t, string(types.SeverityDown), string(outage.Severity))
			}
		})

		t.Run("GET status for component with mixed severity outages returns most severe", func(t *testing.T) {
			// Create outages with different severities for all sub-components
			tideOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)
			deckOutage := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)
			hookOutage := createOutageWithSeverity(t, client, "Prow", "Hook", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			plankOutage := createOutageWithSeverity(t, client, "Prow", "Plank", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)

			// Confirm the Tide outage to test most severe logic
			updateOutage(t, client, "Prow", "Tide", tideOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			status := getStatus(t, client, "Prow", "")

			// Should return Down (most severe), not Degraded
			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 4)
		})

		t.Run("GET status for component with unconfirmed critical sub-component outage returns Suspected", func(t *testing.T) {
			// Tide is critical; unconfirmed outage propagates as Suspected rather than Partial
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)

			status := getStatus(t, client, "Prow", "")

			assert.Equal(t, types.StatusSuspected, status.Status)
			assert.Len(t, status.ActiveOutages, 1)
			assert.False(t, status.ActiveOutages[0].ConfirmedAt.Valid)
		})

		t.Run("GET status for component with mixed confirmed/unconfirmed outages shows confirmed severity", func(t *testing.T) {
			// Create outages for all sub-components (auto-confirmed for Deck, Hook, Plank)
			deckOutage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)
			hookOutage := createOutage(t, client, "Prow", "Hook")
			defer deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			plankOutage := createOutage(t, client, "Prow", "Plank")
			defer deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)

			// Create unconfirmed outage for Tide (requires_confirmation: true)
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)

			status := getStatus(t, client, "Prow", "")

			// Should return Down (confirmed) not Suspected (unconfirmed)
			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 4)
		})

		t.Run("GET status for non-existent component returns 404", func(t *testing.T) {
			expect404(t, client, "/api/status/"+utils.Slugify("NonExistent"), false)
		})
	}
}

func createOutageWithSeverity(t *testing.T, client *TestHTTPClient, componentName, subComponentName, severity string) types.Outage {
	outagePayload := map[string]interface{}{
		"severity":        severity,
		"start_time":      time.Now().UTC().Format(time.RFC3339),
		"description":     fmt.Sprintf("Test outage with %s severity", severity),
		"discovered_from": "e2e-test",
		"created_by":      "developer",
	}

	payloadBytes, err := json.Marshal(outagePayload)
	require.NoError(t, err)

	resp, err := client.Post(fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify(componentName), utils.Slugify(subComponentName)), payloadBytes)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusCreated, resp.StatusCode)

	var outage types.Outage
	err = json.NewDecoder(resp.Body).Decode(&outage)
	require.NoError(t, err)

	// Verify that created_by is set to the user from X-Forwarded-User header
	assert.Equal(t, "developer", outage.CreatedBy, "created_by should be set to the user from X-Forwarded-User header")

	return outage
}
func testAllComponentsStatus(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET status for all components returns all components with their status", func(t *testing.T) {
			allStatuses := getAllComponentsStatus(t, client)

			// Prow, Downstream CI, Build Farm, Boskos, Sippy, Errata Reliability, and TRT Incidents
			assert.Len(t, allStatuses, 7)
			// Find Prow component
			var prowStatus *types.ComponentStatus
			var buildFarmStatus *types.ComponentStatus
			var boskosStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
				}
				if allStatuses[i].ComponentName == "Build Farm" {
					buildFarmStatus = &allStatuses[i]
				}
				if allStatuses[i].ComponentName == "Boskos" {
					boskosStatus = &allStatuses[i]
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			require.NotNil(t, buildFarmStatus, "Build Farm component should be present")
			require.NotNil(t, boskosStatus, "Boskos component should be present")
			assert.Equal(t, types.StatusHealthy, prowStatus.Status)
			assert.Empty(t, prowStatus.ActiveOutages)
			assert.Equal(t, types.StatusHealthy, buildFarmStatus.Status)
			assert.Empty(t, buildFarmStatus.ActiveOutages)
			assert.Equal(t, types.StatusHealthy, boskosStatus.Status)
			assert.Empty(t, boskosStatus.ActiveOutages)
		})

		t.Run("GET status for all components with outages shows correct statuses", func(t *testing.T) {
			// Create outages for all sub-components with mixed severities
			tideOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)
			deckOutage := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)
			hookOutage := createOutageWithSeverity(t, client, "Prow", "Hook", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			plankOutage := createOutageWithSeverity(t, client, "Prow", "Plank", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)

			// Confirm Tide outage (others should be auto-confirmed)
			updateOutage(t, client, "Prow", "Tide", tideOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			allStatuses := getAllComponentsStatus(t, client)

			// Prow, Downstream CI, Build Farm, Boskos, Sippy, Errata Reliability, and TRT Incidents
			assert.Len(t, allStatuses, 7)
			// Find Prow component
			var prowStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
					break
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			assert.Equal(t, "Prow", prowStatus.ComponentName)
			assert.Equal(t, types.StatusDown, prowStatus.Status) // Most severe status
			assert.Len(t, prowStatus.ActiveOutages, 4)
		})

		t.Run("GET status for all components with partial outages shows Partial status", func(t *testing.T) {
			// Create outage for only one sub-component (Deck doesn't require confirmation)
			deckOutage := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)

			allStatuses := getAllComponentsStatus(t, client)

			assert.Len(t, allStatuses, 7)
			// Find Prow component
			var prowStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
					break
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			assert.Equal(t, "Prow", prowStatus.ComponentName)
			assert.Equal(t, types.StatusPartial, prowStatus.Status) // Only one sub-component affected
			assert.Len(t, prowStatus.ActiveOutages, 1)
			assert.Equal(t, string(types.SeverityDegraded), string(prowStatus.ActiveOutages[0].Severity))
		})

		t.Run("GET status for all components with confirmed critical sub-component outage bypasses Partial", func(t *testing.T) {
			tideOutage := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)
			// Confirm Tide outage (requires_confirmation: true)
			updateOutage(t, client, "Prow", "Tide", tideOutage.ID, map[string]interface{}{
				"confirmed": true,
			})

			allStatuses := getAllComponentsStatus(t, client)

			assert.Len(t, allStatuses, 7)
			var prowStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
					break
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			assert.Equal(t, "Prow", prowStatus.ComponentName)
			assert.Equal(t, types.StatusDegraded, prowStatus.Status)
			assert.Len(t, prowStatus.ActiveOutages, 1)
		})

		t.Run("GET status for all components with unconfirmed critical outage shows Suspected", func(t *testing.T) {
			// Create unconfirmed outage for Tide (requires_confirmation: true)
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)

			allStatuses := getAllComponentsStatus(t, client)

			assert.Len(t, allStatuses, 7)
			// Find Prow component
			var prowStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
					break
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			assert.Equal(t, "Prow", prowStatus.ComponentName)
			assert.Equal(t, types.StatusSuspected, prowStatus.Status)
			assert.Len(t, prowStatus.ActiveOutages, 1)
			assert.False(t, prowStatus.ActiveOutages[0].ConfirmedAt.Valid)
		})

		t.Run("GET status for all components with mixed confirmed/unconfirmed outages shows confirmed severity", func(t *testing.T) {
			// Create outages for all sub-components (auto-confirmed for Deck, Hook, Plank)
			deckOutage := createOutage(t, client, "Prow", "Deck")
			defer deleteOutage(t, client, "Prow", "Deck", deckOutage.ID)
			hookOutage := createOutage(t, client, "Prow", "Hook")
			defer deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			plankOutage := createOutage(t, client, "Prow", "Plank")
			defer deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)

			// Create unconfirmed outage for Tide (requires_confirmation: true)
			tideOutage := createOutage(t, client, "Prow", "Tide")
			defer deleteOutage(t, client, "Prow", "Tide", tideOutage.ID)

			allStatuses := getAllComponentsStatus(t, client)

			assert.Len(t, allStatuses, 7)
			// Find Prow component
			var prowStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
					break
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			assert.Equal(t, "Prow", prowStatus.ComponentName)
			// Should return Down (confirmed) not Suspected (unconfirmed)
			assert.Equal(t, types.StatusDown, prowStatus.Status)
			assert.Len(t, prowStatus.ActiveOutages, 4)
		})

		t.Run("GET status for all components includes last_ping_time when available", func(t *testing.T) {
			// Prow should already have a last_ping_time from the initial seed, just verify it
			allStatuses := getAllComponentsStatus(t, client)

			// Find Prow and Build Farm components
			var prowStatus *types.ComponentStatus
			var buildFarmStatus *types.ComponentStatus
			for i := range allStatuses {
				if allStatuses[i].ComponentName == prowComponentName {
					prowStatus = &allStatuses[i]
				}
				if allStatuses[i].ComponentName == "Build Farm" {
					buildFarmStatus = &allStatuses[i]
				}
			}
			require.NotNil(t, prowStatus, "Prow component should be present")
			require.NotNil(t, buildFarmStatus, "Build Farm component should be present")

			// Prow should have last_ping_time since Hook and Plank were pinged at test start
			assert.NotNil(t, prowStatus.LastPingTime, "Prow should have last_ping_time from seeded pings")

			// Build Farm should not have last_ping_time since no report was sent
			assert.Nil(t, buildFarmStatus.LastPingTime, "Build Farm should not have last_ping_time without reports")
		})
	}
}

func testListSubComponents(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("no filters returns all sub-components", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "", "")
			// Prow 4 + Downstream CI 1 + Build Farm 2 + Boskos 2 + Sippy 5 + Errata Reliability 1.
			// TRT Incidents is slo_component and omitted from this list.
			assert.Len(t, subs, 15)
			for _, sub := range subs {
				assert.NotEmpty(t, sub.Status)
			}
		})
		t.Run("componentName filter returns only that component's sub-components", func(t *testing.T) {
			subs := getSubComponents(t, client, "prow", "", "")
			assert.Len(t, subs, 4)
			names := make([]string, len(subs))
			for i := range subs {
				names[i] = subs[i].Name
			}
			assert.ElementsMatch(t, []string{"Tide", "Deck", "Hook", "Plank"}, names)
		})
		t.Run("non-matching componentName returns empty", func(t *testing.T) {
			subs := getSubComponents(t, client, "non-existent", "", "")
			assert.Empty(t, subs)
		})

		t.Run("team filter returns sub-components from components with that team", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "", "TestPlatform")
			// Prow + Downstream CI both have ship_team TestPlatform
			assert.Len(t, subs, 5)
		})
		t.Run("non-matching team returns empty", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "", "NonExistentTeam")
			assert.Empty(t, subs)
		})

		t.Run("tag filter returns sub-components from different components for same tag", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "jobs", "")
			// tag "jobs": Prow (Plank), Downstream CI (Retester), Build Farm (Build01, Build02), Boskos (Quota, Leases)
			assert.Len(t, subs, 6)
			names := make([]string, len(subs))
			for i := range subs {
				names[i] = subs[i].Name
			}
			assert.ElementsMatch(t, []string{"Plank", "Retester", "Build01", "Build02", "Quota", "Leases"}, names)
		})
		t.Run("non-matching tag returns empty", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "nonexistent-tag", "")
			assert.Empty(t, subs)
		})

		t.Run("tag and componentName together filter correctly", func(t *testing.T) {
			subs := getSubComponents(t, client, "prow", "ci", "")
			// Prow sub-components with tag "ci": Tide, Hook, Plank
			assert.Len(t, subs, 3)
			names := make([]string, len(subs))
			for i := range subs {
				names[i] = subs[i].Name
			}
			assert.ElementsMatch(t, []string{"Tide", "Hook", "Plank"}, names)
		})
		t.Run("tag and team together filter correctly", func(t *testing.T) {
			subs := getSubComponents(t, client, "", "ci", "TestPlatform")
			// TestPlatform components with tag "ci": Prow (Tide, Hook, Plank), Downstream CI (Retester)
			assert.Len(t, subs, 4)
			names := make([]string, len(subs))
			for i := range subs {
				names[i] = subs[i].Name
			}
			assert.ElementsMatch(t, []string{"Tide", "Hook", "Plank", "Retester"}, names)
		})

		t.Run("status filter returns only matching sub-components", func(t *testing.T) {
			down := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Deck", down.ID)
			degraded := createOutageWithSeverity(t, client, "Prow", "Tide", string(types.SeverityDegraded))
			defer deleteOutage(t, client, "Prow", "Tide", degraded.ID)

			subs := getSubComponents(t, client, "", "", "", "Down", "Degraded")
			require.GreaterOrEqual(t, len(subs), 2)
			byName := map[string]types.SubComponentListItem{}
			for _, sub := range subs {
				byName[sub.Name] = sub
				assert.NotEmpty(t, sub.Status)
				assert.Contains(t, []types.Status{types.StatusDown, types.StatusDegraded}, sub.Status)
			}
			assert.Equal(t, types.StatusDown, byName["Deck"].Status)
			assert.Equal(t, types.StatusDegraded, byName["Tide"].Status)
		})

		t.Run("status=Healthy excludes unhealthy sub-components", func(t *testing.T) {
			down := createOutageWithSeverity(t, client, "Prow", "Deck", string(types.SeverityDown))
			defer deleteOutage(t, client, "Prow", "Deck", down.ID)

			subs := getSubComponents(t, client, "prow", "", "", "Healthy")
			names := make([]string, len(subs))
			for i := range subs {
				names[i] = subs[i].Name
				assert.Equal(t, types.StatusHealthy, subs[i].Status)
			}
			assert.NotContains(t, names, "Deck")
			assert.Contains(t, names, "Tide")
		})

		t.Run("invalid status returns 400", func(t *testing.T) {
			resp, err := client.Get("/api/sub-components?status=Nope", false)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("status=Partial returns 400", func(t *testing.T) {
			resp, err := client.Get("/api/sub-components?status=Partial", false)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

	}
}

func testTags(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		resp, err := client.Get("/api/tags", false)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		var tags []types.Tag
		err = json.NewDecoder(resp.Body).Decode(&tags)
		require.NoError(t, err)

		// E2E config defines 7 tags with proper capitalization
		assert.Len(t, tags, 7)

		// Check that each tag has required fields
		tagNames := make([]string, len(tags))
		for i, tag := range tags {
			assert.NotEmpty(t, tag.Name)
			assert.NotEmpty(t, tag.Description)
			assert.NotEmpty(t, tag.Color)
			tagNames[i] = tag.Name
		}

		// Verify expected tags are present (with proper capitalization)
		assert.ElementsMatch(t, []string{"CI", "PR-Merging", "Frontend", "GitHub", "Jobs", "AWS", "GCP"}, tagNames)
	}
}

func testUser(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("GET /api/user returns authenticated user", func(t *testing.T) {
			resp, err := client.Get("/api/user", true)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			var userResponse struct {
				Username   string   `json:"username"`
				Components []string `json:"components"`
			}
			err = json.NewDecoder(resp.Body).Decode(&userResponse)
			require.NoError(t, err)

			assert.Equal(t, "developer", userResponse.Username)
			// Components should be a slice (can be empty)
			assert.NotNil(t, userResponse.Components)
			assert.Contains(t, userResponse.Components, utils.Slugify("Prow"), "developer should have access to Prow")
			assert.Contains(t, userResponse.Components, utils.Slugify("Boskos"), "developer should have access to Boskos")
			assert.NotContains(t, userResponse.Components, utils.Slugify("Build Farm"), "developer should not have access to Build Farm")
		})
	}
}

func testAbsentReport(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("Absent report checker creates outage when no ping exists, and resolves when ping is received", func(t *testing.T) {
			var outage *types.Outage
			ctx := context.Background()
			err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
				outages := getOutages(t, client, "Downstream CI", "Retester")
				for i := range outages {
					if outages[i].DiscoveredFrom == "absent-monitored-component-report" && !outages[i].EndTime.Valid {
						outage = &outages[i]
						return true, nil
					}
				}
				return false, nil
			})

			require.NoError(t, err, "Absent report outage should be created within 20 seconds")
			require.NotNil(t, outage, "Absent report outage should exist")
			assert.Equal(t, "downstream-ci", outage.ComponentName)
			assert.Equal(t, "retester", outage.SubComponentName)
			assert.Equal(t, string(types.SeverityDown), string(outage.Severity))
			assert.Equal(t, "absent-monitored-component-report", outage.DiscoveredFrom)
			assert.Equal(t, "dashboard", outage.CreatedBy)
			assert.Contains(t, outage.Description, "Component-monitor has not reported status within expected time")
			assert.True(t, outage.ConfirmedAt.Valid, "Outage should be auto-confirmed since component doesn't require confirmation")

			// Verify the component status reflects the outage
			status := getStatus(t, client, "Downstream CI", "Retester")
			assert.Equal(t, types.StatusDown, status.Status)
			assert.Len(t, status.ActiveOutages, 1)

			// Store outage ID for later verification
			outageID := outage.ID

			// Send a healthy report to create a ping and trigger auto-resolve
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Downstream CI"),
						SubComponentSlug: utils.Slugify("Retester"),
						Status:           types.StatusHealthy,
						Reasons:          []types.Reason{{Type: types.CheckTypePrometheus}},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			reportSentTime := time.Now()
			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Wait for the absent report checker to run and auto-resolve the outage
			// The checker runs every 15s in e2e tests, so wait up to 20s
			var resolvedOutage *types.Outage
			err = wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
				updatedOutages := getOutages(t, client, "Downstream CI", "Retester")
				for i := range updatedOutages {
					if updatedOutages[i].ID == outageID {
						resolvedOutage = &updatedOutages[i]
						if resolvedOutage.EndTime.Valid {
							return true, nil
						}
						break
					}
				}
				return false, nil
			})

			require.NoError(t, err, "Outage should be resolved within 20 seconds")
			require.NotNil(t, resolvedOutage, "Outage should still exist after resolution")
			assert.True(t, resolvedOutage.EndTime.Valid, "Outage should be resolved")

			// Verify the component status is now healthy
			updatedStatus := getStatus(t, client, "Downstream CI", "Retester")
			assert.Equal(t, types.StatusHealthy, updatedStatus.Status)
			assert.Len(t, updatedStatus.ActiveOutages, 0)
			assert.NotNil(t, updatedStatus.LastPingTime, "Should have a last ping time after report")
			assert.WithinDuration(t, reportSentTime, *updatedStatus.LastPingTime, 5*time.Second, "last_ping_time should be within 5 seconds of when report was sent")
		})
	}
}

func testCommunityReport(adminClient *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		componentSlug := utils.Slugify("Prow")
		subComponentSlug := utils.Slugify("Deck")

		reporter1, err := NewTestHTTPClientWithUsername(adminClient.publicURL, adminClient.protectedURL, "reporter1")
		require.NoError(t, err)
		reporter2, err := NewTestHTTPClientWithUsername(adminClient.publicURL, adminClient.protectedURL, "reporter2")
		require.NoError(t, err)

		reportEndpoint := fmt.Sprintf("/api/components/%s/%s/outages/report-suspected", componentSlug, subComponentSlug)
		outageEndpoint := func(id uint) string {
			return fmt.Sprintf("/api/components/%s/%s/outages/%d", componentSlug, subComponentSlug, id)
		}

		type reportResponse struct {
			Outage      *types.Outage `json:"outage"`
			ReportCount int64         `json:"report_count"`
			Created     bool          `json:"created"`
		}

		statusEndpoint := fmt.Sprintf("/api/status/%s/%s", componentSlug, subComponentSlug)
		componentStatusEndpoint := fmt.Sprintf("/api/status/%s", componentSlug)

		t.Run("first report creates a suspected outage", func(t *testing.T) {
			payload, _ := json.Marshal(map[string]string{"description": "Deck UI is not loading"})
			resp, err := reporter1.Post(reportEndpoint, payload)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusCreated, resp.StatusCode)

			var report reportResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&report))
			outage := report.Outage
			assert.True(t, report.Created)
			assert.Equal(t, int64(1), report.ReportCount)
			assert.Equal(t, types.SeveritySuspected, outage.Severity)
			assert.Equal(t, "Deck UI is not loading", outage.Description)
			assert.Equal(t, outage_pkg.CommunityReportSource, outage.DiscoveredFrom)
			assert.False(t, outage.ConfirmedAt.Valid)

			t.Run("suspected outage is excluded from public outage list", func(t *testing.T) {
				outages := getOutages(t, adminClient, "Prow", "Deck")
				for _, o := range outages {
					assert.NotEqual(t, outage.ID, o.ID, "suspected outage should not appear in public list")
				}
			})

			t.Run("sub-component status shows Suspected with suspected_outage info", func(t *testing.T) {
				resp, err := adminClient.Get(statusEndpoint, false)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				var status types.ComponentStatus
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
				assert.Equal(t, types.StatusSuspected, status.Status)
				require.NotNil(t, status.SuspectedOutage, "expected suspected_outage in status response")
				assert.Equal(t, outage.ID, status.SuspectedOutage.OutageID)
				assert.Equal(t, int64(1), status.SuspectedOutage.ReportCount)

				for _, o := range status.ActiveOutages {
					assert.NotEqual(t, outage.ID, o.ID, "suspected outage should not appear in active_outages")
				}
			})

			t.Run("component status shows Suspected for sub-component", func(t *testing.T) {
				resp, err := adminClient.Get(componentStatusEndpoint, false)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				var status types.ComponentStatus
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
				require.Contains(t, status.SubComponentStatuses, subComponentSlug)
				assert.Equal(t, types.StatusSuspected, status.SubComponentStatuses[subComponentSlug])
			})

			t.Run("suspected outage is accessible by direct ID", func(t *testing.T) {
				resp, err := adminClient.Get(outageEndpoint(outage.ID), false)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				var fetched types.Outage
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&fetched))
				assert.Equal(t, outage.ID, fetched.ID)
				assert.Equal(t, types.SeveritySuspected, fetched.Severity)
			})

			t.Run("duplicate report from same user returns conflict", func(t *testing.T) {
				resp, err := reporter1.Post(reportEndpoint, nil)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusConflict, resp.StatusCode)

				var errResp map[string]string
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
				assert.Contains(t, errResp["error"], "already reported")
			})

			t.Run("second report from different user upgrades to degraded (threshold=2)", func(t *testing.T) {
				resp, err := reporter2.Post(reportEndpoint, nil)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusCreated, resp.StatusCode)

				var report reportResponse
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&report))
				assert.False(t, report.Created)
				assert.Equal(t, int64(2), report.ReportCount)
				assert.Equal(t, outage.ID, report.Outage.ID)
				assert.Equal(t, types.SeverityDegraded, report.Outage.Severity)
			})

			t.Run("after upgrade status shows Degraded without suspected_outage", func(t *testing.T) {
				resp, err := adminClient.Get(statusEndpoint, false)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusOK, resp.StatusCode)

				var status types.ComponentStatus
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&status))
				assert.Equal(t, types.StatusDegraded, status.Status)
				assert.Nil(t, status.SuspectedOutage, "suspected_outage should be absent after upgrade")
			})

			t.Run("upgraded outage now appears in public list", func(t *testing.T) {
				outages := getOutages(t, adminClient, "Prow", "Deck")
				found := false
				for _, o := range outages {
					if o.ID == outage.ID {
						found = true
						assert.Equal(t, types.SeverityDegraded, o.Severity)
						break
					}
				}
				assert.True(t, found, "upgraded outage should appear in public list")
			})

			t.Run("report when confirmed outage exists returns conflict", func(t *testing.T) {
				reporter3, err := NewTestHTTPClientWithUsername(adminClient.publicURL, adminClient.protectedURL, "reporter3")
				require.NoError(t, err)
				resp, err := reporter3.Post(reportEndpoint, nil)
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, http.StatusConflict, resp.StatusCode)

				var errResp map[string]string
				require.NoError(t, json.NewDecoder(resp.Body).Decode(&errResp))
				assert.Contains(t, errResp["error"], "already being tracked")
			})

			deleteOutage(t, adminClient, "Prow", "Deck", outage.ID)
		})

		t.Run("report on non-existent sub-component returns 404", func(t *testing.T) {
			resp, err := reporter1.Post(fmt.Sprintf("/api/components/%s/nonexistent/outages/report-suspected", componentSlug), nil)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
		})
	}
}

// Prepares the test data by ensuring no outages exist for absent pings from prior to the tests starting
func cleanupAbsentReportOutages(t *testing.T, client *TestHTTPClient) {
	// Seed pings for monitored sub-components to prevent absent report checker from creating outages
	sendAllClearPing(t, client, "Prow", "Hook")
	sendAllClearPing(t, client, "Prow", "Plank")
	sendAllClearPing(t, client, "TRT Incidents", "Incidents")

	// Clean up any outages created by absent report checker before the pings were seeded
	deleteOutagesFromAbsentReport(t, client, "Prow", "Hook")
	deleteOutagesFromAbsentReport(t, client, "Prow", "Plank")
	deleteOutagesFromAbsentReport(t, client, "TRT Incidents", "Incidents")
}

// sendAllClearPing sends a healthy component monitor report to seed a ping in the database,
// preventing the absent report checker from creating an immediate outage.
func sendAllClearPing(t *testing.T, client *TestHTTPClient, componentName, subComponentName string) {
	t.Helper()
	postComponentMonitorReport(t, client, componentMonitorSAToken, types.ComponentMonitorReportRequest{
		ComponentMonitor: "app-ci-component-monitor",
		Statuses: []types.ComponentMonitorReportComponentStatus{
			{
				ComponentSlug:    utils.Slugify(componentName),
				SubComponentSlug: utils.Slugify(subComponentName),
				Status:           types.StatusHealthy,
				Reasons:          []types.Reason{{Type: types.CheckTypePrometheus}},
			},
		},
	})
}

// cleanupAbsentReportOutages deletes any outages created by the absent report checker for a given component.
func deleteOutagesFromAbsentReport(t *testing.T, client *TestHTTPClient, componentName, subComponentName string) {
	outages := getOutages(t, client, componentName, subComponentName)
	for _, outage := range outages {
		if outage.DiscoveredFrom == "absent-monitored-component-report" {
			deleteOutage(t, client, componentName, subComponentName, outage.ID)
		}
	}
}

func updateDashboardConfig(t *testing.T, modifier func(*types.DashboardConfig)) {
	configPath := os.Getenv("TEST_DASHBOARD_CONFIG_PATH")
	require.NotEmpty(t, configPath, "TEST_DASHBOARD_CONFIG_PATH must be set")
	modifyConfig(t, configPath, modifier)
}

func testConfigHotReload(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		configPath := os.Getenv("TEST_DASHBOARD_CONFIG_PATH")
		require.NotEmpty(t, configPath, "TEST_DASHBOARD_CONFIG_PATH must be set")

		originalConfig := readConfig(t, configPath)

		defer func() {
			restoreConfig(t, configPath, originalConfig)
		}()

		t.Run("Config changes are reflected after reload", func(t *testing.T) {
			updateDashboardConfig(t, func(config *types.DashboardConfig) {
				// Update existing component description
				var prowFound bool
				for _, comp := range config.Components {
					if comp.Name == "Prow" {
						comp.Description = "Updated description for hot-reload test"
						prowFound = true
						break
					}
				}
				require.True(t, prowFound, "Prow component should exist in config")

				// Add new component
				newComponent := &types.Component{
					Name:        "Test Component",
					Description: "A test component for hot-reload",
					ShipTeam:    "TestTeam",
					SlackReporting: []types.SlackReportingConfig{
						{Channel: "#test-channel", Severity: &[]types.Severity{types.SeverityDown}[0]},
					},
					Subcomponents: []types.SubComponent{
						{
							Name:        "TestSub",
							Description: "Test sub-component",
						},
					},
					Owners: []types.Owner{
						{
							User: "developer",
						},
					},
				}
				config.Components = append(config.Components, newComponent)
			})

			// Wait for config to reload and verify both changes
			ctx := context.Background()
			err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 20*time.Second, true, func(ctx context.Context) (bool, error) {
				// Check that Prow description was updated
				prowComponent := getComponent(t, client, "Prow")
				if prowComponent.Description != "Updated description for hot-reload test" {
					return false, nil
				}

				// Check that new component appears
				components := getComponents(t, client)
				var testComponentFound bool
				for _, comp := range components {
					if comp.Name == "Test Component" {
						testComponentFound = true
						break
					}
				}
				return testComponentFound, nil
			})
			require.NoError(t, err, "Config changes should be reflected within 20 seconds")

			// Verify Prow description change
			prowComponent := getComponent(t, client, "Prow")
			assert.Equal(t, "Updated description for hot-reload test", prowComponent.Description)

			// Verify the new component exists
			testComponent := getComponent(t, client, "Test Component")
			assert.Equal(t, "Test Component", testComponent.Name)
			assert.Equal(t, "A test component for hot-reload", testComponent.Description)
			assert.Len(t, testComponent.Subcomponents, 1)
			assert.Equal(t, "TestSub", testComponent.Subcomponents[0].Name)
		})

		t.Run("removing sub-component with active outage resolves orphan and component becomes Healthy", func(t *testing.T) {
			createOutage(t, client, "Boskos", "Leases")

			statusBefore := getStatus(t, client, "Boskos", "")
			assert.Equal(t, types.StatusPartial, statusBefore.Status, "expected Partial when one of two sub-components has an active outage")

			updateDashboardConfig(t, func(config *types.DashboardConfig) {
				var boskosFound bool
				for _, comp := range config.Components {
					if comp.Name != "Boskos" {
						continue
					}
					boskosFound = true
					var kept []types.SubComponent
					for _, sub := range comp.Subcomponents {
						if sub.Name != "Leases" {
							kept = append(kept, sub)
						}
					}
					comp.Subcomponents = kept
					break
				}
				require.True(t, boskosFound, "Boskos component should exist in config")
			})

			ctx := context.Background()
			err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, 90*time.Second, true, func(ctx context.Context) (bool, error) {
				st, ok := tryGetComponentStatus(client, "Boskos")
				if !ok {
					return false, nil
				}
				return st == types.StatusHealthy, nil
			})
			require.NoError(t, err, "Boskos should become Healthy after removed sub-component's outage is resolved")

			finalStatus := getStatus(t, client, "Boskos", "")
			assert.Equal(t, types.StatusHealthy, finalStatus.Status)
			assert.Empty(t, finalStatus.ActiveOutages)
		})
	}
}

func testTriageNotes(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		outage := createOutage(t, client, "Prow", "Deck")
		defer deleteOutage(t, client, "Prow", "Deck", outage.ID)

		basePath := fmt.Sprintf("/api/components/%s/%s/outages/%d/triage-notes",
			utils.Slugify("Prow"), utils.Slugify("Deck"), outage.ID)

		t.Run("POST creates a triage note", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": "Investigating the issue"})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusCreated, resp.StatusCode)

			var note types.TriageNote
			err = json.NewDecoder(resp.Body).Decode(&note)
			require.NoError(t, err)
			assert.Equal(t, "Investigating the issue", note.Body)
			assert.Equal(t, "developer", note.Author)
			assert.Equal(t, outage.ID, note.OutageID)
			assert.NotZero(t, note.ID)
		})

		t.Run("POST rejects empty body", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": ""})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("GET lists triage notes", func(t *testing.T) {
			resp, err := client.Get(basePath, false)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var notes []types.TriageNote
			err = json.NewDecoder(resp.Body).Decode(&notes)
			require.NoError(t, err)
			assert.NotEmpty(t, notes)
			for _, n := range notes {
				assert.Equal(t, outage.ID, n.OutageID)
				assert.NotEmpty(t, n.Body)
			}
		})

		t.Run("PATCH updates a triage note", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": "Original"})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.TriageNote
			json.NewDecoder(resp.Body).Decode(&created)

			updateBody, _ := json.Marshal(map[string]string{"body": "Revised note"})
			notePath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Patch(notePath, updateBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			var updated types.TriageNote
			json.NewDecoder(resp2.Body).Decode(&updated)
			assert.Equal(t, "Revised note", updated.Body)
		})

		t.Run("PATCH with empty body returns 400", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": "For validation"})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.TriageNote
			json.NewDecoder(resp.Body).Decode(&created)

			emptyBody, _ := json.Marshal(map[string]string{"body": ""})
			notePath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Patch(notePath, emptyBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
		})

		t.Run("DELETE removes a triage note", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": "To be deleted"})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.TriageNote
			json.NewDecoder(resp.Body).Decode(&created)

			notePath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Delete(notePath)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusNoContent, resp2.StatusCode)
		})

		t.Run("cross-component access returns 404", func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"body": "Cross-component test"})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.TriageNote
			json.NewDecoder(resp.Body).Decode(&created)

			wrongPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/triage-notes/%d",
				utils.Slugify("Boskos"), utils.Slugify("Quota"), outage.ID, created.ID)
			updateBody, _ := json.Marshal(map[string]string{"body": "hacked"})
			resp2, err := client.Patch(wrongPath, updateBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
		})

		t.Run("non-admin non-author returns 403 on PATCH and DELETE", func(t *testing.T) {
			serverURL := os.Getenv("TEST_SERVER_URL")
			mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
			require.NotEmpty(t, serverURL)
			require.NotEmpty(t, mockOauthProxyURL)

			// Create note on Boskos (developer is admin, editor is not)
			boskosOutage := createOutage(t, client, "Boskos", "Quota")
			defer deleteOutage(t, client, "Boskos", "Quota", boskosOutage.ID)

			boskosNotePath := fmt.Sprintf("/api/components/%s/%s/outages/%d/triage-notes",
				utils.Slugify("Boskos"), utils.Slugify("Quota"), boskosOutage.ID)
			noteBody, _ := json.Marshal(map[string]string{"body": "Developer's note"})
			resp, err := client.Post(boskosNotePath, noteBody)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.TriageNote
			json.NewDecoder(resp.Body).Decode(&created)

			editorClient, err := NewTestHTTPClientWithUsername(serverURL, mockOauthProxyURL, "editor")
			require.NoError(t, err)

			notePath := fmt.Sprintf("%s/%d", boskosNotePath, created.ID)

			updateBody, _ := json.Marshal(map[string]string{"body": "Unauthorized edit"})
			resp2, err := editorClient.Patch(notePath, updateBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp2.StatusCode)

			resp3, err := editorClient.Delete(notePath)
			require.NoError(t, err)
			defer resp3.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp3.StatusCode)
		})

		t.Run("CreateOutage with initial_triage_note", func(t *testing.T) {
			payload := map[string]interface{}{
				"severity":            string(types.SeverityDown),
				"start_time":          time.Now().UTC().Format(time.RFC3339),
				"description":         "Outage with initial note",
				"discovered_from":     "e2e-test",
				"initial_triage_note": "First observations",
			}
			payloadBytes, _ := json.Marshal(payload)
			resp, err := client.Post(fmt.Sprintf("/api/components/%s/%s/outages",
				utils.Slugify("Prow"), utils.Slugify("Deck")), payloadBytes)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.Outage
			json.NewDecoder(resp.Body).Decode(&created)
			defer deleteOutage(t, client, "Prow", "Deck", created.ID)

			getResp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d",
				utils.Slugify("Prow"), utils.Slugify("Deck"), created.ID), false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var fetched types.Outage
			json.NewDecoder(getResp.Body).Decode(&fetched)
			require.Len(t, fetched.TriageNotes, 1)
			assert.Equal(t, "First observations", fetched.TriageNotes[0].Body)
			assert.Equal(t, "developer", fetched.TriageNotes[0].Author)
		})
	}
}

func testOutageLinks(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		outage := createOutage(t, client, "Prow", "Deck")
		defer deleteOutage(t, client, "Prow", "Deck", outage.ID)

		basePath := fmt.Sprintf("/api/components/%s/%s/outages/%d/links",
			utils.Slugify("Prow"), utils.Slugify("Deck"), outage.ID)

		t.Run("POST creates an outage link", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://example.com/rca",
				LinkType: "rca",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusCreated, resp.StatusCode)

			var link types.OutageLink
			err = json.NewDecoder(resp.Body).Decode(&link)
			require.NoError(t, err)
			assert.Equal(t, "https://example.com/rca", link.URL)
			assert.Equal(t, types.LinkTypeRCA, link.LinkType)
			assert.Empty(t, link.Description)
			assert.Equal(t, outage.ID, link.OutageID)
		})

		t.Run("POST rejects invalid URL", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "not-a-url",
				LinkType: "rca",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("POST rejects invalid link type", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://example.com",
				LinkType: "invalid_type",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("GET lists outage links", func(t *testing.T) {
			resp, err := client.Get(basePath, false)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			var links []types.OutageLink
			err = json.NewDecoder(resp.Body).Decode(&links)
			require.NoError(t, err)
			assert.NotEmpty(t, links)
			for _, l := range links {
				assert.Equal(t, outage.ID, l.OutageID)
				assert.NotEmpty(t, l.URL)
			}
		})

		t.Run("PATCH updates an outage link", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:         "https://old.example.com",
				LinkType:    "other",
				Description: "Original",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)
			assert.Equal(t, "Original", created.Description)

			updateBody, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://new.example.com/rca",
				LinkType: "rca",
			})
			linkPath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Patch(linkPath, updateBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			var updated types.OutageLink
			json.NewDecoder(resp2.Body).Decode(&updated)
			assert.Equal(t, "https://new.example.com/rca", updated.URL)
			assert.Equal(t, types.LinkTypeRCA, updated.LinkType)
			assert.Empty(t, updated.Description)
		})

		t.Run("PATCH with invalid URL returns 400", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://example.com/patch-val",
				LinkType: "other",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)

			invalidBody, _ := json.Marshal(types.OutageLinkRequest{URL: "not-valid", LinkType: "rca"})
			linkPath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Patch(linkPath, invalidBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
		})

		t.Run("PATCH with invalid link type returns 400", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://example.com/type-val",
				LinkType: "other",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)

			invalidBody, _ := json.Marshal(types.OutageLinkRequest{URL: "https://example.com", LinkType: "bogus"})
			linkPath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Patch(linkPath, invalidBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp2.StatusCode)
		})

		t.Run("DELETE removes an outage link", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:         "https://example.com/to-delete",
				LinkType:    "other",
				Description: "Will be removed",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)

			linkPath := fmt.Sprintf("%s/%d", basePath, created.ID)
			resp2, err := client.Delete(linkPath)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusNoContent, resp2.StatusCode)
		})

		t.Run("cross-component access returns 404", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:      "https://example.com/cross-comp",
				LinkType: "rca",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)

			wrongPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/links/%d",
				utils.Slugify("Boskos"), utils.Slugify("Quota"), outage.ID, created.ID)
			updateBody, _ := json.Marshal(types.OutageLinkRequest{URL: "https://example.com/hacked", LinkType: "rca"})
			resp2, err := client.Patch(wrongPath, updateBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusNotFound, resp2.StatusCode)
		})

		t.Run("unauthorized user returns 403 on POST, PATCH, and DELETE", func(t *testing.T) {
			serverURL := os.Getenv("TEST_SERVER_URL")
			mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
			require.NotEmpty(t, serverURL)
			require.NotEmpty(t, mockOauthProxyURL)

			// Create link on Boskos (developer is admin, editor is not)
			boskosOutage := createOutage(t, client, "Boskos", "Quota")
			defer deleteOutage(t, client, "Boskos", "Quota", boskosOutage.ID)

			boskosLinkPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/links",
				utils.Slugify("Boskos"), utils.Slugify("Quota"), boskosOutage.ID)
			linkBody, _ := json.Marshal(types.OutageLinkRequest{URL: "https://example.com/boskos", LinkType: "rca"})
			resp, err := client.Post(boskosLinkPath, linkBody)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageLink
			json.NewDecoder(resp.Body).Decode(&created)

			editorClient, err := NewTestHTTPClientWithUsername(serverURL, mockOauthProxyURL, "editor")
			require.NoError(t, err)

			// POST forbidden
			newBody, _ := json.Marshal(types.OutageLinkRequest{URL: "https://example.com/unauth", LinkType: "other"})
			resp2, err := editorClient.Post(boskosLinkPath, newBody)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp2.StatusCode)

			// PATCH forbidden
			linkPath := fmt.Sprintf("%s/%d", boskosLinkPath, created.ID)
			resp3, err := editorClient.Patch(linkPath, newBody)
			require.NoError(t, err)
			defer resp3.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp3.StatusCode)

			// DELETE forbidden
			resp4, err := editorClient.Delete(linkPath)
			require.NoError(t, err)
			defer resp4.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp4.StatusCode)
		})

		t.Run("outage GET includes links", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageLinkRequest{
				URL:         "https://example.com/verify",
				LinkType:    "incident_channel_thread",
				Description: "Thread",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			getResp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d",
				utils.Slugify("Prow"), utils.Slugify("Deck"), outage.ID), false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var fetched types.Outage
			err = json.NewDecoder(getResp.Body).Decode(&fetched)
			require.NoError(t, err)
			require.NotEmpty(t, fetched.Links)

			var found bool
			for _, l := range fetched.Links {
				if l.URL == "https://example.com/verify" {
					found = true
					assert.Equal(t, types.LinkTypeIncidentChannelThread, l.LinkType)
				}
			}
			assert.True(t, found, "expected link to appear in outage GET response")
		})
	}
}

func testOutageRelationships(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		outageA := createOutage(t, client, "Prow", "Deck")
		defer deleteOutage(t, client, "Prow", "Deck", outageA.ID)

		outageB := createOutage(t, client, "Prow", "Tide")
		defer deleteOutage(t, client, "Prow", "Tide", outageB.ID)

		basePath := fmt.Sprintf("/api/components/%s/%s/outages/%d/relationships",
			utils.Slugify("Prow"), utils.Slugify("Deck"), outageA.ID)

		t.Run("POST creates an outage relationship", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "causes",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var rel types.OutageRelationship
			err = json.NewDecoder(resp.Body).Decode(&rel)
			require.NoError(t, err)
			assert.Equal(t, outageA.ID, rel.OutageID)
			assert.Equal(t, outageB.ID, rel.RelatedOutageID)
			assert.Equal(t, types.RelationshipCauses, rel.RelationshipType)

			// Verify reciprocal exists on outage B
			reciprocalPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/relationships",
				utils.Slugify("Prow"), utils.Slugify("Tide"), outageB.ID)
			getResp, err := client.Get(reciprocalPath, false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var reciprocals []types.OutageRelationship
			err = json.NewDecoder(getResp.Body).Decode(&reciprocals)
			require.NoError(t, err)
			require.Len(t, reciprocals, 1)
			assert.Equal(t, types.RelationshipCausedBy, reciprocals[0].RelationshipType)
			assert.Equal(t, outageA.ID, reciprocals[0].RelatedOutageID)

			// Clean up: delete from side A
			delPath := fmt.Sprintf("%s/%d", basePath, rel.ID)
			delResp, err := client.Delete(delPath)
			require.NoError(t, err)
			defer delResp.Body.Close()
			assert.Equal(t, http.StatusNoContent, delResp.StatusCode)
		})

		t.Run("POST rejects self-link", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageA.ID,
				RelationshipType: "related_to",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("POST rejects invalid relationship type", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "invalid_type",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("POST rejects nonexistent target outage", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  999999,
				RelationshipType: "related_to",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("POST rejects duplicate relationship", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "related_to",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageRelationship
			json.NewDecoder(resp.Body).Decode(&created)
			defer func() {
				delPath := fmt.Sprintf("%s/%d", basePath, created.ID)
				delResp, _ := client.Delete(delPath)
				defer delResp.Body.Close()
			}()

			resp2, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp2.Body.Close()
			assert.Equal(t, http.StatusConflict, resp2.StatusCode)
		})

		t.Run("GET lists outage relationships", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "related_to",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageRelationship
			json.NewDecoder(resp.Body).Decode(&created)

			getResp, err := client.Get(basePath, false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var rels []types.OutageRelationship
			err = json.NewDecoder(getResp.Body).Decode(&rels)
			require.NoError(t, err)
			require.NotEmpty(t, rels)

			// Clean up
			delPath := fmt.Sprintf("%s/%d", basePath, created.ID)
			delResp, _ := client.Delete(delPath)
			defer delResp.Body.Close()
		})

		t.Run("DELETE removes relationship and reciprocal", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "causes",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var rel types.OutageRelationship
			json.NewDecoder(resp.Body).Decode(&rel)

			delPath := fmt.Sprintf("%s/%d", basePath, rel.ID)
			delResp, err := client.Delete(delPath)
			require.NoError(t, err)
			defer delResp.Body.Close()
			assert.Equal(t, http.StatusNoContent, delResp.StatusCode)

			// Verify no relationships on either side
			getResp, err := client.Get(basePath, false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			var remaining []types.OutageRelationship
			json.NewDecoder(getResp.Body).Decode(&remaining)
			assert.Empty(t, remaining)

			reciprocalPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/relationships",
				utils.Slugify("Prow"), utils.Slugify("Tide"), outageB.ID)
			getResp2, err := client.Get(reciprocalPath, false)
			require.NoError(t, err)
			defer getResp2.Body.Close()
			var reciprocals []types.OutageRelationship
			json.NewDecoder(getResp2.Body).Decode(&reciprocals)
			assert.Empty(t, reciprocals)
		})

		t.Run("outage GET includes relationships", func(t *testing.T) {
			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageB.ID,
				RelationshipType: "related_to",
			})
			resp, err := client.Post(basePath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var created types.OutageRelationship
			json.NewDecoder(resp.Body).Decode(&created)
			defer func() {
				delPath := fmt.Sprintf("%s/%d", basePath, created.ID)
				delResp, _ := client.Delete(delPath)
				defer delResp.Body.Close()
			}()

			getResp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d",
				utils.Slugify("Prow"), utils.Slugify("Deck"), outageA.ID), false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var fetched types.Outage
			err = json.NewDecoder(getResp.Body).Decode(&fetched)
			require.NoError(t, err)
			require.NotEmpty(t, fetched.Relationships)
			assert.Equal(t, outageB.ID, fetched.Relationships[0].RelatedOutageID)
			require.NotNil(t, fetched.Relationships[0].RelatedOutage, "RelatedOutage must be preloaded on GET")
			assert.Equal(t, outageB.ID, fetched.Relationships[0].RelatedOutage.ID)
			assert.Equal(t, utils.Slugify("Prow"), fetched.Relationships[0].RelatedOutage.ComponentName)
			assert.Equal(t, utils.Slugify("Tide"), fetched.Relationships[0].RelatedOutage.SubComponentName)
		})

		t.Run("outage GET preloads related outage across components", func(t *testing.T) {
			outageC := createOutage(t, client, "Boskos", "Quota")
			defer deleteOutage(t, client, "Boskos", "Quota", outageC.ID)

			crossBody, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageC.ID,
				RelationshipType: "caused_by",
			})
			crossResp, err := client.Post(basePath, crossBody)
			require.NoError(t, err)
			defer crossResp.Body.Close()
			require.Equal(t, http.StatusCreated, crossResp.StatusCode)

			var crossRel types.OutageRelationship
			json.NewDecoder(crossResp.Body).Decode(&crossRel)
			defer func() {
				delPath := fmt.Sprintf("%s/%d", basePath, crossRel.ID)
				delResp, _ := client.Delete(delPath)
				defer delResp.Body.Close()
			}()

			getResp, err := client.Get(fmt.Sprintf("/api/components/%s/%s/outages/%d",
				utils.Slugify("Prow"), utils.Slugify("Deck"), outageA.ID), false)
			require.NoError(t, err)
			defer getResp.Body.Close()
			require.Equal(t, http.StatusOK, getResp.StatusCode)

			var fetched types.Outage
			err = json.NewDecoder(getResp.Body).Decode(&fetched)
			require.NoError(t, err)

			var found bool
			for _, rel := range fetched.Relationships {
				if rel.RelatedOutageID == outageC.ID {
					found = true
					require.NotNil(t, rel.RelatedOutage, "cross-component RelatedOutage must be preloaded")
					assert.Equal(t, utils.Slugify("Boskos"), rel.RelatedOutage.ComponentName)
					assert.Equal(t, utils.Slugify("Quota"), rel.RelatedOutage.SubComponentName)
					break
				}
			}
			assert.True(t, found, "cross-component relationship not found in outage GET")
		})

		t.Run("unauthorized user rejected", func(t *testing.T) {
			serverURL := os.Getenv("TEST_SERVER_URL")
			mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
			require.NotEmpty(t, serverURL)
			require.NotEmpty(t, mockOauthProxyURL)

			boskosOutage := createOutage(t, client, "Boskos", "Quota")
			defer deleteOutage(t, client, "Boskos", "Quota", boskosOutage.ID)

			boskosPath := fmt.Sprintf("/api/components/%s/%s/outages/%d/relationships",
				utils.Slugify("Boskos"), utils.Slugify("Quota"), boskosOutage.ID)

			editorClient, err := NewTestHTTPClientWithUsername(serverURL, mockOauthProxyURL, "editor")
			require.NoError(t, err)

			body, _ := json.Marshal(types.OutageRelationshipRequest{
				RelatedOutageID:  outageA.ID,
				RelationshipType: "related_to",
			})
			resp, err := editorClient.Post(boskosPath, body)
			require.NoError(t, err)
			defer resp.Body.Close()
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})
	}
}

func testServiceAccountOutages(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("SA rejected on unowned component", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Should be rejected",
				"discovered_from": "mcp",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Build Farm"), utils.Slugify("Build01")),
				payloadBytes, chaiBotSAToken, "chai-bot",
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})
	}
}

func testDelegatedAuthorization(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("delegator creates outage on behalf of authorized user", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Delegated outage creation",
				"discovered_from": "mcp",
				"confirmed":       true,
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")),
				payloadBytes, mcpServerSAToken, "developer",
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var outage types.Outage
			err = json.NewDecoder(resp.Body).Decode(&outage)
			require.NoError(t, err)

			assert.NotZero(t, outage.ID)
			assert.Equal(t, "developer", outage.CreatedBy)
			assert.Equal(t, "mcp", outage.DiscoveredFrom)

			// Verify audit log records the delegated user
			auditResp, err := client.Get(
				fmt.Sprintf("/api/components/%s/%s/outages/%d/audit-logs", utils.Slugify("Prow"), utils.Slugify("Tide"), outage.ID),
				false,
			)
			require.NoError(t, err)
			defer auditResp.Body.Close()
			assert.Equal(t, http.StatusOK, auditResp.StatusCode)

			var auditLogs []map[string]interface{}
			err = json.NewDecoder(auditResp.Body).Decode(&auditLogs)
			require.NoError(t, err)
			require.NotEmpty(t, auditLogs)
			assert.Equal(t, "developer", auditLogs[0]["user"])

			deleteOutage(t, client, "Prow", "Tide", outage.ID)
		})

		t.Run("delegator rejected for unauthorized acting_for user", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Should be rejected",
				"discovered_from": "mcp",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")),
				payloadBytes, mcpServerSAToken, "stranger",
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
		})

		t.Run("delegator without acting_for gets 400", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Missing acting_for",
				"discovered_from": "mcp",
			}

			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")),
				payloadBytes, mcpServerSAToken,
			)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
		})

		t.Run("delegator can add triage note on behalf of user", func(t *testing.T) {
			outagePayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Outage for triage note test",
				"discovered_from": "mcp",
				"confirmed":       true,
			}
			payloadBytes, err := json.Marshal(outagePayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")),
				payloadBytes, mcpServerSAToken, "developer",
			)
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusCreated, resp.StatusCode)

			var outage types.Outage
			err = json.NewDecoder(resp.Body).Decode(&outage)
			require.NoError(t, err)
			defer deleteOutage(t, client, "Prow", "Tide", outage.ID)

			notePayload := map[string]interface{}{
				"body": "Investigating the issue via MCP",
			}
			noteBytes, err := json.Marshal(notePayload)
			require.NoError(t, err)

			noteResp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages/%d/triage-notes", utils.Slugify("Prow"), utils.Slugify("Tide"), outage.ID),
				noteBytes, mcpServerSAToken, "developer",
			)
			require.NoError(t, err)
			defer noteResp.Body.Close()

			assert.Equal(t, http.StatusCreated, noteResp.StatusCode)

			var note map[string]interface{}
			err = json.NewDecoder(noteResp.Body).Decode(&note)
			require.NoError(t, err)
			assert.Equal(t, "developer", note["author"])
		})

		t.Run("delegator can delete outage on behalf of user", func(t *testing.T) {
			createPayload := map[string]interface{}{
				"severity":        string(types.SeverityDown),
				"start_time":      time.Now().UTC().Format(time.RFC3339),
				"description":     "Outage for delegated delete test",
				"discovered_from": "mcp",
				"confirmed":       true,
			}
			createBytes, err := json.Marshal(createPayload)
			require.NoError(t, err)

			createResp, err := client.PostWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages", utils.Slugify("Prow"), utils.Slugify("Tide")),
				createBytes, mcpServerSAToken, "developer",
			)
			require.NoError(t, err)
			defer createResp.Body.Close()
			require.Equal(t, http.StatusCreated, createResp.StatusCode)

			var outage types.Outage
			err = json.NewDecoder(createResp.Body).Decode(&outage)
			require.NoError(t, err)

			deleteResp, err := client.DeleteWithBearerToken(
				fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), outage.ID),
				mcpServerSAToken, "developer",
			)
			require.NoError(t, err)
			defer deleteResp.Body.Close()

			assert.Equal(t, http.StatusNoContent, deleteResp.StatusCode)

			getResp, err := client.Get(
				fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify("Prow"), utils.Slugify("Tide"), outage.ID),
				false,
			)
			require.NoError(t, err)
			defer getResp.Body.Close()
			assert.Equal(t, http.StatusNotFound, getResp.StatusCode)
		})
	}
}
