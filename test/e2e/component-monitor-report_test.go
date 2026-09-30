//nolint:errcheck,unparam // Test helpers - error handling and unused parameters are acceptable in test code
package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"testing"
	"time"

	"ship-status-dash/pkg/types"
	"ship-status-dash/pkg/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_ComponentMonitorReport(t *testing.T) {
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

	t.Run("ComponentMonitorReport", testComponentMonitorReport(client))
	t.Run("ComponentMonitorPerReasonReport", testComponentMonitorPerReasonReport(client))
}

func testComponentMonitorReport(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		t.Run("POST report with Down status creates outage", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
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

			var response map[string]string
			err = json.NewDecoder(resp.Body).Decode(&response)
			require.NoError(t, err)
			assert.Equal(t, "processed", response["status"])

			// Verify outage was created
			outages := getOutages(t, client, "Prow", "Hook")
			var foundOutage *types.Outage
			for i := range outages {
				if outages[i].DiscoveredFrom == "component-monitor" && len(outages[i].Reasons) > 0 && outages[i].Reasons[0].Type == types.CheckTypePrometheus {
					foundOutage = &outages[i]
					break
				}
			}
			require.NotNil(t, foundOutage, "Outage should be created")
			assert.Equal(t, string(types.SeverityDown), string(foundOutage.Severity))
			assert.Equal(t, "component-monitor", foundOutage.DiscoveredFrom)
			assert.Equal(t, "app-ci-component-monitor", foundOutage.CreatedBy)
			require.Len(t, foundOutage.Reasons, 1)
			assert.Equal(t, types.CheckTypePrometheus, foundOutage.Reasons[0].Type)
			assert.Equal(t, "up{job=\"hook\"} == 0", foundOutage.Reasons[0].Check)
			assert.Equal(t, "No healthy instances found", foundOutage.Reasons[0].Results)

			// Verify that ping time was set
			status := getStatus(t, client, "Prow", "Hook")
			assert.NotNil(t, status.LastPingTime, "last_ping_time should be set after component monitor report")
			assert.WithinDuration(t, reportSentTime, *status.LastPingTime, 5*time.Second, "last_ping_time should be within 5 seconds of when report was sent")

			// Cleanup
			deleteOutage(t, client, "Prow", "Hook", foundOutage.ID)
		})

		t.Run("POST report with Degraded status creates outage", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDegraded,
						Reasons: []types.Reason{
							{
								Type:    "http",
								Check:   "https://hook.example.com/health",
								Results: "Response time > 5s",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify outage was created
			outages := getOutages(t, client, "Prow", "Hook")
			var foundOutage *types.Outage
			for i := range outages {
				if outages[i].DiscoveredFrom == "component-monitor" && len(outages[i].Reasons) > 0 && outages[i].Reasons[0].Type == types.CheckTypeHTTP {
					foundOutage = &outages[i]
					break
				}
			}
			require.NotNil(t, foundOutage, "Outage should be created")
			assert.Equal(t, string(types.SeverityDegraded), string(foundOutage.Severity))

			// Cleanup
			deleteOutage(t, client, "Prow", "Hook", foundOutage.ID)
		})

		t.Run("POST report does not create duplicate outage for same Reason.Type", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			// First report
			resp1, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp1.Body.Close()
			assert.Equal(t, http.StatusOK, resp1.StatusCode)

			// Get the created outage
			outages1 := getOutages(t, client, "Prow", "Hook")
			var firstOutage *types.Outage
			for i := range outages1 {
				if outages1[i].DiscoveredFrom == "component-monitor" && len(outages1[i].Reasons) > 0 && outages1[i].Reasons[0].Type == "prometheus" {
					firstOutage = &outages1[i]
					break
				}
			}
			require.NotNil(t, firstOutage, "First outage should be created")

			// Second report with same Reason.Type
			resp2, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			// Verify no duplicate was created
			outages2 := getOutages(t, client, "Prow", "Hook")
			count := 0
			for i := range outages2 {
				if outages2[i].DiscoveredFrom == "component-monitor" && len(outages2[i].Reasons) > 0 && outages2[i].Reasons[0].Type == "prometheus" && outages2[i].EndTime.Valid == false {
					count++
				}
			}
			assert.Equal(t, 1, count, "Should only have one active outage created by the same component-monitor")

			// Cleanup
			deleteOutage(t, client, "Prow", "Hook", firstOutage.ID)
		})

		t.Run("POST report with Healthy status auto-resolves outage when auto_resolve is true", func(t *testing.T) {
			// Create an outage first
			downReport := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"), // Hook has auto_resolve: true
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			downBytes, err := json.Marshal(downReport)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", downBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Get the created outage
			outages := getOutages(t, client, "Prow", "Hook")
			var outage *types.Outage
			for i := range outages {
				if outages[i].DiscoveredFrom == "component-monitor" && len(outages[i].Reasons) > 0 && outages[i].Reasons[0].Type == "prometheus" && !outages[i].EndTime.Valid {
					outage = &outages[i]
					break
				}
			}
			require.NotNil(t, outage, "Outage should be created")
			assert.False(t, outage.EndTime.Valid, "Outage should be active")

			// Now report healthy status
			healthyReport := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusHealthy,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "All instances healthy",
							},
						},
					},
				},
			}

			healthyBytes, err := json.Marshal(healthyReport)
			require.NoError(t, err)

			reportSentTime := time.Now()
			resp2, err := client.PostWithBearerToken("/api/component-monitor/report", healthyBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			// Verify outage was resolved
			outages2 := getOutages(t, client, "Prow", "Hook")
			var resolvedOutage *types.Outage
			for i := range outages2 {
				if outages2[i].ID == outage.ID {
					resolvedOutage = &outages2[i]
					break
				}
			}
			require.NotNil(t, resolvedOutage, "Outage should still exist")
			assert.True(t, resolvedOutage.EndTime.Valid, "Outage should be resolved")

			// Verify that ping time was updated
			status := getStatus(t, client, "Prow", "Hook")
			assert.NotNil(t, status.LastPingTime, "last_ping_time should be set after component monitor report")
			assert.WithinDuration(t, reportSentTime, *status.LastPingTime, 5*time.Second, "last_ping_time should be within 5 seconds of when report was sent")
		})

		t.Run("POST report with Healthy status does not resolve when auto_resolve is false", func(t *testing.T) {
			// Create an outage first for Plank (which has auto_resolve: false)
			downReport := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Plank"), // Plank has auto_resolve: false
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"plank\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			downBytes, err := json.Marshal(downReport)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", downBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp.Body.Close()
			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Get the created outage
			outages := getOutages(t, client, "Prow", "Plank")
			var outage *types.Outage
			for i := range outages {
				if outages[i].DiscoveredFrom == "component-monitor" && len(outages[i].Reasons) > 0 && outages[i].Reasons[0].Type == "prometheus" && !outages[i].EndTime.Valid {
					outage = &outages[i]
					break
				}
			}
			require.NotNil(t, outage, "Outage should be created")

			// Now report healthy status
			healthyReport := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Plank"),
						Status:           types.StatusHealthy,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"plank\"} == 0",
								Results: "All instances healthy",
							},
						},
					},
				},
			}

			healthyBytes, err := json.Marshal(healthyReport)
			require.NoError(t, err)

			resp2, err := client.PostWithBearerToken("/api/component-monitor/report", healthyBytes, componentMonitorSAToken)
			require.NoError(t, err)
			resp2.Body.Close()
			assert.Equal(t, http.StatusOK, resp2.StatusCode)

			// Verify outage was NOT resolved
			outages2 := getOutages(t, client, "Prow", "Plank")
			var stillActiveOutage *types.Outage
			for i := range outages2 {
				if outages2[i].ID == outage.ID {
					stillActiveOutage = &outages2[i]
					break
				}
			}
			require.NotNil(t, stillActiveOutage, "Outage should still exist")
			assert.False(t, stillActiveOutage.EndTime.Valid, "Outage should still be active")

			// Cleanup
			deleteOutage(t, client, "Prow", "Plank", outage.ID)
		})

		t.Run("POST report with invalid component returns 400", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("NonExistentComponent"),
						SubComponentSlug: utils.Slugify("Deck"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"deck\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Contains(t, errorResponse["error"], "Component not found")
		})

		t.Run("POST report with invalid sub-component returns 400", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("NonExistentSub"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"deck\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Contains(t, errorResponse["error"], "Sub-component not found")
		})

		t.Run("POST report with multiple statuses processes all", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Plank"),
						Status:           types.StatusDegraded,
						Reasons: []types.Reason{
							{
								Type:    "http",
								Check:   "https://plank.example.com/health",
								Results: "Response time > 5s",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Verify both outages were created
			hookOutages := getOutages(t, client, "Prow", "Hook")
			var hookOutage *types.Outage
			for i := range hookOutages {
				if hookOutages[i].DiscoveredFrom == "component-monitor" && len(hookOutages[i].Reasons) > 0 && hookOutages[i].Reasons[0].Type == "prometheus" {
					hookOutage = &hookOutages[i]
					break
				}
			}
			require.NotNil(t, hookOutage, "Hook outage should be created")

			plankOutages := getOutages(t, client, "Prow", "Plank")
			var plankOutage *types.Outage
			for i := range plankOutages {
				if plankOutages[i].DiscoveredFrom == "component-monitor" && len(plankOutages[i].Reasons) > 0 && plankOutages[i].Reasons[0].Type == "http" {
					plankOutage = &plankOutages[i]
					break
				}
			}
			require.NotNil(t, plankOutage, "Plank outage should be created")

			// Cleanup
			deleteOutage(t, client, "Prow", "Hook", hookOutage.ID)
			deleteOutage(t, client, "Prow", "Plank", plankOutage.ID)
		})

		t.Run("POST report with empty component_monitor returns 400", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Contains(t, errorResponse["error"], "component_monitor is required")
		})

		t.Run("POST report with empty statuses returns 400", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses:         []types.ComponentMonitorReportComponentStatus{},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Contains(t, errorResponse["error"], "statuses cannot be empty")
		})

		t.Run("POST report with invalid token returns 401", func(t *testing.T) {
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			invalidToken := "invalid-token"
			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, invalidToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})

		t.Run("POST report with service account not an owner returns 400", func(t *testing.T) {
			// Build Farm component does not have the service account as an owner
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "app-ci-component-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Build Farm"),
						SubComponentSlug: utils.Slugify("Build01"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"build01\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Equal(t, "Invalid request", errorResponse["error"])
		})

		t.Run("POST report with wrong component monitor instance returns 400", func(t *testing.T) {
			// Prow/Hook is configured for "app-ci-component-monitor", not "wrong-monitor"
			reportPayload := types.ComponentMonitorReportRequest{
				ComponentMonitor: "wrong-monitor",
				Statuses: []types.ComponentMonitorReportComponentStatus{
					{
						ComponentSlug:    utils.Slugify("Prow"),
						SubComponentSlug: utils.Slugify("Hook"),
						Status:           types.StatusDown,
						Reasons: []types.Reason{
							{
								Type:    "prometheus",
								Check:   "up{job=\"hook\"} == 0",
								Results: "No healthy instances found",
							},
						},
					},
				},
			}

			payloadBytes, err := json.Marshal(reportPayload)
			require.NoError(t, err)

			resp, err := client.PostWithBearerToken("/api/component-monitor/report", payloadBytes, componentMonitorSAToken)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

			var errorResponse map[string]string
			err = json.NewDecoder(resp.Body).Decode(&errorResponse)
			require.NoError(t, err)
			assert.Equal(t, "Invalid request", errorResponse["error"])
		})
	}
}

const (
	trtIncidentsComponent = "TRT Incidents"
	trtIncidentsSub       = "Incidents"
)

func jiraBrowseURL(key string) string {
	return "https://redhat.atlassian.net/browse/" + key
}

func jiraReportReason(key, summary string, withLink bool) types.Reason {
	reason := types.Reason{
		Type:    types.CheckTypeJira,
		Check:   key,
		Results: summary,
	}
	if withLink {
		reason.Links = []types.ReportedLink{{
			URL:      jiraBrowseURL(key),
			LinkType: types.LinkTypeJira,
		}}
	}
	return reason
}

func postTRTIncidentReport(t *testing.T, client *TestHTTPClient, status types.Status, reasons []types.Reason) {
	t.Helper()
	postComponentMonitorReport(t, client, componentMonitorSAToken, types.ComponentMonitorReportRequest{
		ComponentMonitor: "app-ci-component-monitor",
		Statuses: []types.ComponentMonitorReportComponentStatus{
			{
				ComponentSlug:    utils.Slugify(trtIncidentsComponent),
				SubComponentSlug: utils.Slugify(trtIncidentsSub),
				Status:           status,
				Reasons:          reasons,
			},
		},
	})
}

func activeJiraOutagesByCheck(outages []types.Outage) map[string]types.Outage {
	byCheck := make(map[string]types.Outage)
	for _, outage := range outages {
		if outage.EndTime.Valid || outage.CreatedBy != "app-ci-component-monitor" || outage.DiscoveredFrom != "component-monitor" {
			continue
		}
		if len(outage.Reasons) == 0 || outage.Reasons[0].Type != types.CheckTypeJira {
			continue
		}
		byCheck[outage.Reasons[0].Check] = outage
	}
	return byCheck
}

func jiraOutageByCheck(outages []types.Outage, check string) *types.Outage {
	var found *types.Outage
	for i := range outages {
		outage := &outages[i]
		if outage.CreatedBy != "app-ci-component-monitor" || outage.DiscoveredFrom != "component-monitor" {
			continue
		}
		if len(outage.Reasons) == 0 || outage.Reasons[0].Type != types.CheckTypeJira || outage.Reasons[0].Check != check {
			continue
		}
		if found == nil || outage.ID > found.ID {
			found = outage
		}
	}
	return found
}

func testComponentMonitorPerReasonReport(client *TestHTTPClient) func(*testing.T) {
	return func(t *testing.T) {
		cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")

		t.Run("two Jira reasons create two active outages with summaries and links", func(t *testing.T) {
			cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")
			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-1", "First incident", true),
				jiraReportReason("TRT-2", "Second incident", true),
			})

			byCheck := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Len(t, byCheck, 2)
			first, ok := byCheck["TRT-1"]
			require.True(t, ok, "expected active outage for TRT-1")
			second, ok := byCheck["TRT-2"]
			require.True(t, ok, "expected active outage for TRT-2")
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, first.ID)
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, second.ID)

			assert.Equal(t, "First incident", first.Description)
			assert.Equal(t, "Second incident", second.Description)
			assert.Equal(t, "app-ci-component-monitor", first.CreatedBy)
			assert.Equal(t, "app-ci-component-monitor", second.CreatedBy)
			assert.Equal(t, string(types.SeverityDegraded), string(first.Severity))
			assert.Equal(t, string(types.SeverityDegraded), string(second.Severity))

			fetchedFirst := getOutage(t, client, trtIncidentsComponent, trtIncidentsSub, first.ID)
			require.Len(t, fetchedFirst.Links, 1)
			assert.Equal(t, jiraBrowseURL("TRT-1"), fetchedFirst.Links[0].URL)
			assert.Equal(t, types.LinkTypeJira, fetchedFirst.Links[0].LinkType)

			fetchedSecond := getOutage(t, client, trtIncidentsComponent, trtIncidentsSub, second.ID)
			require.Len(t, fetchedSecond.Links, 1)
			assert.Equal(t, jiraBrowseURL("TRT-2"), fetchedSecond.Links[0].URL)
			assert.Equal(t, types.LinkTypeJira, fetchedSecond.Links[0].LinkType)
		})

		t.Run("second report with only TRT-2 auto-resolves TRT-1", func(t *testing.T) {
			cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")
			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-1", "First incident", true),
				jiraReportReason("TRT-2", "Second incident", true),
			})
			created := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Len(t, created, 2)
			firstID := created["TRT-1"].ID
			secondID := created["TRT-2"].ID
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, firstID)
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, secondID)

			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-2", "Second incident", true),
			})

			outages := getOutages(t, client, trtIncidentsComponent, trtIncidentsSub)
			active := activeJiraOutagesByCheck(outages)
			require.Len(t, active, 1)
			_, ok := active["TRT-2"]
			require.True(t, ok, "TRT-2 should stay active")
			_, ok = active["TRT-1"]
			require.False(t, ok, "TRT-1 should no longer be active")

			resolved := jiraOutageByCheck(outages, "TRT-1")
			require.NotNil(t, resolved)
			assert.Equal(t, firstID, resolved.ID)
			assert.True(t, resolved.EndTime.Valid)
		})

		t.Run("empty healthy reasons auto-resolve remaining outage", func(t *testing.T) {
			cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")
			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-2", "Second incident", true),
			})
			created := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Contains(t, created, "TRT-2")
			remainingID := created["TRT-2"].ID
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, remainingID)

			postTRTIncidentReport(t, client, types.StatusHealthy, nil)

			outages := getOutages(t, client, trtIncidentsComponent, trtIncidentsSub)
			assert.Empty(t, activeJiraOutagesByCheck(outages))
			resolved := jiraOutageByCheck(outages, "TRT-2")
			require.NotNil(t, resolved)
			assert.Equal(t, remainingID, resolved.ID)
			assert.True(t, resolved.EndTime.Valid)
		})

		t.Run("reporting the same two keys again does not duplicate", func(t *testing.T) {
			cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")
			reasons := []types.Reason{
				jiraReportReason("TRT-1", "First incident", true),
				jiraReportReason("TRT-2", "Second incident", true),
			}
			postTRTIncidentReport(t, client, types.StatusDegraded, reasons)
			first := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Len(t, first, 2)
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, first["TRT-1"].ID)
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, first["TRT-2"].ID)

			postTRTIncidentReport(t, client, types.StatusDegraded, reasons)

			second := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Len(t, second, 2)
			assert.Equal(t, first["TRT-1"].ID, second["TRT-1"].ID)
			assert.Equal(t, first["TRT-2"].ID, second["TRT-2"].ID)
		})

		t.Run("reporting links later backfills an existing outage", func(t *testing.T) {
			cleanupOutages(t, client, trtIncidentsComponent, trtIncidentsSub, "app-ci-component-monitor")
			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-1", "First incident", false),
			})
			created := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Contains(t, created, "TRT-1")
			outageID := created["TRT-1"].ID
			defer deleteOutage(t, client, trtIncidentsComponent, trtIncidentsSub, outageID)

			assert.Empty(t, getOutage(t, client, trtIncidentsComponent, trtIncidentsSub, outageID).Links)

			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-1", "First incident", true),
			})

			fetched := getOutage(t, client, trtIncidentsComponent, trtIncidentsSub, outageID)
			require.Len(t, fetched.Links, 1)
			assert.Equal(t, jiraBrowseURL("TRT-1"), fetched.Links[0].URL)
			assert.Equal(t, types.LinkTypeJira, fetched.Links[0].LinkType)

			postTRTIncidentReport(t, client, types.StatusDegraded, []types.Reason{
				jiraReportReason("TRT-1", "First incident", true),
			})
			assert.Len(t, getOutage(t, client, trtIncidentsComponent, trtIncidentsSub, outageID).Links, 1)

			active := activeJiraOutagesByCheck(getOutages(t, client, trtIncidentsComponent, trtIncidentsSub))
			require.Len(t, active, 1)
			assert.Equal(t, outageID, active["TRT-1"].ID)
		})
	}
}
