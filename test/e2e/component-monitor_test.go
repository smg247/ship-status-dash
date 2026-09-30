//nolint:errcheck,unparam // Test helpers - error handling and unused parameters are acceptable in test code
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"ship-status-dash/pkg/types"
	"ship-status-dash/pkg/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/wait"
)

var allHealthyMetrics = map[string]interface{}{
	"success_rate": 1.0, // >= 0.9, so query succeeds for api sub-component
	"data_load_failure": map[string]float64{
		"api":   0.0, // < 1, so query succeeds for data-load sub-component
		"db":    0.0, // < 1, so query succeeds
		"cache": 0.0, // < 1, so query succeeds
	},
}

func TestE2E_ComponentMonitor(t *testing.T) {
	serverURL := os.Getenv("TEST_SERVER_URL")
	if serverURL == "" {
		t.Fatalf("TEST_SERVER_URL is not set")
	}
	mockOauthProxyURL := os.Getenv("TEST_MOCK_OAUTH_PROXY_URL")
	if mockOauthProxyURL == "" {
		t.Fatalf("TEST_MOCK_OAUTH_PROXY_URL is not set")
	}
	mockMonitoredComponentURL := os.Getenv("TEST_MOCK_MONITORED_COMPONENT_URL")
	if mockMonitoredComponentURL == "" {
		t.Fatalf("TEST_MOCK_MONITORED_COMPONENT_URL is not set")
	}

	client, err := NewTestHTTPClient(serverURL, mockOauthProxyURL)
	require.NoError(t, err)

	prometheusURL := os.Getenv("TEST_PROMETHEUS_URL")
	if prometheusURL == "" {
		t.Fatalf("TEST_PROMETHEUS_URL is not set")
	}

	t.Run("HTTPComponentMonitorProbe", testHTTPComponentMonitorProbe(client, mockMonitoredComponentURL))
	t.Run("PrometheusComponentMonitorProbe", testPrometheusComponentMonitorProbe(client, mockMonitoredComponentURL, prometheusURL))
	t.Run("ConfigHotReload", testComponentMonitorConfigHotReload(client, mockMonitoredComponentURL, prometheusURL))
}

func testHTTPComponentMonitorProbe(client *TestHTTPClient, mockMonitoredComponentURL string) func(*testing.T) {
	return func(t *testing.T) {
		// Clean up any existing outages first (component-monitor may have created them)
		cleanupActiveOutages(t, client, "Sippy", "Sippy")

		// Ensure service starts in healthy state
		resp, err := http.Get(mockMonitoredComponentURL + "/health")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Verify no active outages exist initially
		outages := getOutages(t, client, "Sippy", "Sippy")
		activeOutages := filterActiveOutages(outages)
		assert.Empty(t, activeOutages, "Should have no active outages initially")

		// Bring service down
		resp, err = http.Get(mockMonitoredComponentURL + "/down")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for component-monitor to detect the failure and create an outage
		foundOutage := waitForOutageCreated(t, client, "Sippy", "Sippy", 30*time.Second)

		// Verify outage properties
		assert.Equal(t, string(types.SeverityDown), string(foundOutage.Severity))
		assert.Equal(t, "component-monitor", foundOutage.DiscoveredFrom)
		assert.Equal(t, "e2e-component-monitor", foundOutage.CreatedBy)
		require.Equal(t, len(foundOutage.Reasons), 1, "Outage should have one reason")
		assert.Equal(t, types.CheckTypeHTTP, foundOutage.Reasons[0].Type)
		assert.Equal(t, foundOutage.Reasons[0].Type, types.CheckTypeHTTP)
		assert.False(t, foundOutage.EndTime.Valid, "Outage should not be resolved yet")

		// Bring service back up
		resp, err = http.Get(mockMonitoredComponentURL + "/up")
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		resp.Body.Close()

		// Wait for component-monitor to detect the recovery and resolve the outage
		waitForOutageResolved(t, client, "Sippy", "Sippy", foundOutage.ID, 30*time.Second)

		// Verify the previously found outage is now resolved
		// Get all outages and find the resolved one
		outages = getOutages(t, client, "Sippy", "Sippy")
		var resolvedOutage *types.Outage
		for i := range outages {
			if outages[i].ID == foundOutage.ID {
				resolvedOutage = &outages[i]
				break
			}
		}
		require.NotNil(t, resolvedOutage, "Should find the previously created outage")

		assert.True(t, resolvedOutage.EndTime.Valid, "Outage should be resolved with EndTime set")
	}
}

func testPrometheusComponentMonitorProbe(client *TestHTTPClient, mockMonitoredComponentURL, prometheusURL string) func(*testing.T) {
	return func(t *testing.T) {
		componentName := "Sippy"

		// Clean up any existing outages first (component-monitor may have created them before Prometheus had metrics)
		cleanupActiveOutages(t, client, componentName, "api")
		cleanupActiveOutages(t, client, componentName, "data-load")

		// Set up all healthy metrics once at the beginning
		updateMetrics(t, mockMonitoredComponentURL, allHealthyMetrics)

		// Test sub-component with single range query (api)
		t.Run("SingleRangeQuery", func(t *testing.T) {
			subComponentName := "api"
			expectedReasons := []types.Reason{
				{
					Type:    types.CheckTypePrometheus,
					Check:   "success_rate >= 0.9",
					Results: "0.5",
				},
			}

			cleanupActiveOutages(t, client, componentName, subComponentName)
			verifyNoActiveOutages(t, client, componentName, subComponentName, "initially")
			waitAndVerifySuccessfulProbe(t, client, componentName, subComponentName)

			unhealthyMetrics := map[string]interface{}{
				"success_rate": 0.5, // < 0.9, so range query returns empty matrix (fails)
			}
			setUnhealthyMetrics(t, mockMonitoredComponentURL, unhealthyMetrics)
			foundOutage := waitForOutageCreated(t, client, componentName, subComponentName, 60*time.Second)
			verifyOutageReasons(t, foundOutage, expectedReasons)
			assert.Equal(t, types.SeverityDegraded, foundOutage.Severity, "Outage should have Degraded severity as configured")

			restoreHealthyMetricsAndVerifyRecovery(t, mockMonitoredComponentURL, client, componentName, subComponentName, foundOutage.ID)
		})

		// Test sub-component with multiple queries (data-load)
		t.Run("MultipleInstantQueries", func(t *testing.T) {
			subComponentName := "data-load"
			expectedReasons := []types.Reason{
				{
					Type:    types.CheckTypePrometheus,
					Check:   "data_load_failure{component=\"api\"} < 1",
					Results: "1",
				},
				{
					Type:    types.CheckTypePrometheus,
					Check:   "data_load_failure{component=\"db\"} < 1",
					Results: "1",
				},
				{
					Type:    types.CheckTypePrometheus,
					Check:   "data_load_failure{component=\"cache\"} < 1",
					Results: "1",
				},
			}

			cleanupActiveOutages(t, client, componentName, subComponentName)
			verifyNoActiveOutages(t, client, componentName, subComponentName, "initially")
			waitAndVerifySuccessfulProbe(t, client, componentName, subComponentName)

			unhealthyMetrics := map[string]interface{}{
				"data_load_failure": map[string]float64{
					"api":   1.0, // >= 1, so query returns empty vector (fails)
					"db":    1.0, // >= 1, so query returns empty vector (fails)
					"cache": 1.0, // >= 1, so query returns empty vector (fails)
				},
			}
			setUnhealthyMetrics(t, mockMonitoredComponentURL, unhealthyMetrics)
			foundOutage := waitForOutageCreated(t, client, componentName, subComponentName, 30*time.Second)
			verifyOutageReasons(t, foundOutage, expectedReasons)
			assert.Equal(t, types.SeverityDegraded, foundOutage.Severity, "Outage should have Degraded severity as configured")

			restoreHealthyMetricsAndVerifyRecovery(t, mockMonitoredComponentURL, client, componentName, subComponentName, foundOutage.ID)
		})

		// Test sub-component with multiple queries where one always fails with an error
		t.Run("QueryErrorPreventsStatusReport", func(t *testing.T) {
			subComponentName := "error-handling-test"
			componentName := "Sippy"

			// Clean up any existing outages
			cleanupActiveOutages(t, client, componentName, subComponentName)

			// Set up healthy metrics for the valid query
			updateMetrics(t, mockMonitoredComponentURL, allHealthyMetrics)

			// Wait for component-monitor to attempt probing (which will fail due to query error)
			// Then wait for absent report checker to detect missing report and create outage
			// The absent report checker runs every 15s in e2e tests, and threshold is 5x frequency (50s)
			// So we need to wait at least 65s for the absent report checker to create an outage
			var absentReportOutage *types.Outage
			ctx := context.Background()
			err := wait.PollUntilContextTimeout(ctx, 2*time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
				outages := getOutages(t, client, componentName, subComponentName)
				for i := range outages {
					if outages[i].DiscoveredFrom == "absent-monitored-component-report" && !outages[i].EndTime.Valid {
						absentReportOutage = &outages[i]
						return true, nil
					}
				}
				return false, nil
			})

			require.NoError(t, err, "Absent report outage should be created when no status is sent due to query error")
			require.NotNil(t, absentReportOutage, "Absent report outage should exist")

			assert.Equal(t, string(types.SeverityDown), string(absentReportOutage.Severity))
			assert.Equal(t, "absent-monitored-component-report", absentReportOutage.DiscoveredFrom)
			assert.Equal(t, "dashboard", absentReportOutage.CreatedBy)
			assert.Contains(t, absentReportOutage.Description, "Component-monitor has not reported status within expected time")
		})
	}
}

func verifyNoActiveOutages(t *testing.T, client *TestHTTPClient, componentName, subComponentName, context string) {
	outages := getOutages(t, client, componentName, subComponentName)
	activeOutages := filterActiveOutages(outages)
	assert.Empty(t, activeOutages, "Should have no active outages %s", context)
}

func waitAndVerifySuccessfulProbe(t *testing.T, client *TestHTTPClient, componentName, subComponentName string) {
	// Wait for component-monitor to detect healthy state (no active outages)
	waitForNoActiveOutages(t, client, componentName, subComponentName, 30*time.Second)
}

func setUnhealthyMetrics(t *testing.T, mockMonitoredComponentURL string, metrics map[string]interface{}) {
	updateMetrics(t, mockMonitoredComponentURL, metrics)
}

func verifyOutageReasons(t *testing.T, foundOutage *types.Outage, expectedReasons []types.Reason) {
	require.Equal(t, len(expectedReasons), len(foundOutage.Reasons), "Outage should have exactly %d reasons (one per failed query)", len(expectedReasons))

	// Create a map to track which expected reasons have been matched
	matched := make(map[int]bool, len(expectedReasons))

	// For each actual reason, find a matching expected reason
	for i, reason := range foundOutage.Reasons {
		assert.Equal(t, types.CheckTypePrometheus, reason.Type, "Reason %d should be Prometheus type", i)
		assert.NotEmpty(t, reason.Check, "Reason %d should have a query", i)
		assert.NotEmpty(t, reason.Results, "Reason %d should have results", i)

		// Find a matching expected reason
		found := false
		for j, expected := range expectedReasons {
			if matched[j] {
				continue
			}
			if expected.Type == reason.Type && expected.Check == reason.Check && expected.Results == reason.Results {
				matched[j] = true
				found = true
				break
			}
		}
		assert.True(t, found, "Reason %d should match one of the expected reasons", i)
	}

	// Verify all expected reasons were matched
	for i, expected := range expectedReasons {
		assert.True(t, matched[i], "Expected reason %d (Type: %s, Check: %s, Results: %s) should be present", i, expected.Type, expected.Check, expected.Results)
	}
}

func restoreHealthyMetricsAndVerifyRecovery(t *testing.T, mockMonitoredComponentURL string, client *TestHTTPClient, componentName, subComponentName string, outageId uint) {
	updateMetrics(t, mockMonitoredComponentURL, allHealthyMetrics)
	// Wait for component-monitor to detect recovery and resolve the outage
	waitForOutageResolved(t, client, componentName, subComponentName, outageId, 30*time.Second)

	verifyNoActiveOutages(t, client, componentName, subComponentName, "after Prometheus queries recover")

	// Verify the previously found outage is now resolved
	outages := getOutages(t, client, componentName, subComponentName)
	var resolvedOutage *types.Outage
	for i := range outages {
		if outages[i].ID == outageId {
			resolvedOutage = &outages[i]
			break
		}
	}
	require.NotNil(t, resolvedOutage, "Should find the previously created outage")
	assert.True(t, resolvedOutage.EndTime.Valid, "Outage should be resolved with EndTime set")
}

func updateMetrics(t *testing.T, baseURL string, metrics map[string]interface{}) {
	jsonData, err := json.Marshal(metrics)
	require.NoError(t, err)

	resp, err := http.Post(baseURL+"/update-metrics", "application/json", bytes.NewBuffer(jsonData))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()
}

// filterActiveOutages filters outages to only return those that are currently active (not resolved)
// TODO: at some point we should have a filter for the endpoint that only returns active outages
func filterActiveOutages(outages []types.Outage) []types.Outage {
	var active []types.Outage
	for i := range outages {
		if !outages[i].EndTime.Valid {
			active = append(active, outages[i])
		}
	}
	return active
}

// cleanupActiveOutages deletes any existing active outages for a component/subcomponent
func cleanupActiveOutages(t *testing.T, client *TestHTTPClient, componentName, subComponentName string) {
	outages := getOutages(t, client, componentName, subComponentName)
	activeOutages := filterActiveOutages(outages)
	for _, outage := range activeOutages {
		resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/%d", utils.Slugify(componentName), utils.Slugify(subComponentName), outage.ID))
		if err == nil && resp.StatusCode == http.StatusNoContent {
			resp.Body.Close()
		}
	}
	// Wait a bit after cleanup to ensure the deletion is processed
	if len(activeOutages) > 0 {
		time.Sleep(2 * time.Second)
	}
}

// cleanupOutages deletes outages for a component/sub-component, including resolved ones.
// createdBy limits the delete to that creator. An empty value deletes every outage.
func cleanupOutages(t *testing.T, client *TestHTTPClient, componentName, subComponentName, createdBy string) {
	t.Helper()
	outages := getOutages(t, client, componentName, subComponentName)
	deleted := 0
	for _, outage := range outages {
		if createdBy != "" && outage.CreatedBy != createdBy {
			continue
		}
		resp, err := client.Delete(fmt.Sprintf("/api/components/%s/%s/outages/%d",
			utils.Slugify(componentName), utils.Slugify(subComponentName), outage.ID))
		require.NoError(t, err, "delete outage %d", outage.ID)
		require.Equal(t, http.StatusNoContent, resp.StatusCode, "delete outage %d", outage.ID)
		resp.Body.Close()
		deleted++
	}
	if deleted > 0 {
		time.Sleep(2 * time.Second)
	}
}

// waitForOutageCreated polls the outage API until an outage is created by component-monitor or times out.
func waitForOutageCreated(t *testing.T, client *TestHTTPClient, componentName, subComponentName string, timeout time.Duration) *types.Outage {
	var foundOutage *types.Outage
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		outages := getOutages(t, client, componentName, subComponentName)
		activeOutages := filterActiveOutages(outages)

		for i := range activeOutages {
			if activeOutages[i].DiscoveredFrom == "component-monitor" &&
				activeOutages[i].ComponentName == utils.Slugify(componentName) &&
				activeOutages[i].SubComponentName == utils.Slugify(subComponentName) {
				foundOutage = &activeOutages[i]
				assert.Equal(t, "component-monitor", foundOutage.DiscoveredFrom)
				assert.Equal(t, "e2e-component-monitor", foundOutage.CreatedBy)
				return true, nil
			}
		}
		return false, nil
	})

	if err != nil {
		t.Fatalf("Timeout waiting for component-monitor to create outage for %s/%s: %v", componentName, subComponentName, err)
	}
	return foundOutage
}

// waitForOutageResolved polls the outage API until the specified outage is resolved or times out.
func waitForOutageResolved(t *testing.T, client *TestHTTPClient, componentName, subComponentName string, outageId uint, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		outages := getOutages(t, client, componentName, subComponentName)
		for i := range outages {
			if outages[i].ID == outageId {
				if outages[i].EndTime.Valid {
					return true, nil // Outage is resolved
				}
				break
			}
		}
		return false, nil
	})

	if err != nil {
		t.Fatalf("Timeout waiting for outage %d to be resolved for %s/%s: %v", outageId, componentName, subComponentName, err)
	}
}

// waitForNoActiveOutages polls the outage API until there are no active outages or times out.
func waitForNoActiveOutages(t *testing.T, client *TestHTTPClient, componentName, subComponentName string, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err := wait.PollUntilContextTimeout(ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		outages := getOutages(t, client, componentName, subComponentName)
		activeOutages := filterActiveOutages(outages)
		return len(activeOutages) == 0, nil
	})

	if err != nil {
		t.Fatalf("Timeout waiting for no active outages for %s/%s: %v", componentName, subComponentName, err)
	}
}

func updateComponentMonitorConfig(t *testing.T, modifier func(*types.ComponentMonitorConfig)) {
	configPath := os.Getenv("TEST_COMPONENT_MONITOR_CONFIG_PATH")
	require.NotEmpty(t, configPath, "TEST_COMPONENT_MONITOR_CONFIG_PATH must be set")
	modifyConfig(t, configPath, modifier)
}

func testComponentMonitorConfigHotReload(client *TestHTTPClient, mockMonitoredComponentURL, prometheusURL string) func(*testing.T) {
	return func(t *testing.T) {
		configPath := os.Getenv("TEST_COMPONENT_MONITOR_CONFIG_PATH")
		require.NotEmpty(t, configPath, "TEST_COMPONENT_MONITOR_CONFIG_PATH must be set")

		// Read original config
		originalConfig := readConfig(t, configPath)

		// Restore original config at the end
		defer func() {
			restoreConfig(t, configPath, originalConfig)
			// Wait a bit for config to reload
			time.Sleep(1 * time.Second)
		}()

		t.Run("Component addition is reflected after config reload", func(t *testing.T) {
			// Clean up any existing outages for sippy-chat
			cleanupActiveOutages(t, client, "Sippy", "sippy-chat")

			// Add monitoring for sippy-chat (which exists in dashboard config but not in component-monitor config)
			updateComponentMonitorConfig(t, func(config *types.ComponentMonitorConfig) {
				// Find the existing sippy/sippy component to copy its configuration.
				// We copy instead of constructing manually to ensure we use the exact same URL
				// that was substituted during setup (service URL in CI, not localhost).
				// This avoids needing to determine the correct URL or add/modify env vars.
				var sippyComponent *types.MonitoringComponent
				for i := range config.Components {
					if config.Components[i].ComponentSlug == "sippy" && config.Components[i].SubComponentSlug == "sippy" {
						sippyComponent = &config.Components[i]
						break
					}
				}
				require.NotNil(t, sippyComponent, "Should find existing sippy/sippy component to copy")

				newComponent := types.MonitoringComponent{
					ComponentSlug:    sippyComponent.ComponentSlug,
					SubComponentSlug: "sippy-chat",
				}
				if sippyComponent.HTTPMonitor != nil {
					httpMonitor := *sippyComponent.HTTPMonitor
					newComponent.HTTPMonitor = &httpMonitor
				}
				if sippyComponent.PrometheusMonitor != nil {
					promMonitor := *sippyComponent.PrometheusMonitor
					newComponent.PrometheusMonitor = &promMonitor
				}

				config.Components = append(config.Components, newComponent)
			})

			// Verify the new component is being monitored by bringing down the service
			resp, err := http.Get(mockMonitoredComponentURL + "/down")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			resp.Body.Close()

			// Wait for component-monitor to detect the failure
			foundOutage := waitForOutageCreated(t, client, "Sippy", "sippy-chat", 30*time.Second)
			require.NotNil(t, foundOutage, "New component should be monitored after config reload")

			// Cleanup
			resp, err = http.Get(mockMonitoredComponentURL + "/up")
			require.NoError(t, err)
			resp.Body.Close()
			waitForOutageResolved(t, client, "Sippy", "sippy-chat", foundOutage.ID, 30*time.Second)
			cleanupActiveOutages(t, client, "Sippy", "sippy-chat")

			// Remove the component from config to restore original state
			restoreConfig(t, configPath, originalConfig)
			// Wait a bit for config to reload
			time.Sleep(1 * time.Second)
		})

		t.Run("Component removal stops monitoring after config reload", func(t *testing.T) {
			// Clean up any existing outages first
			cleanupActiveOutages(t, client, "Sippy", "sippy")

			// Verify the component is currently being monitored by bringing it down
			resp, err := http.Get(mockMonitoredComponentURL + "/down")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			resp.Body.Close()

			// Wait for component-monitor to detect the failure
			outageBefore := waitForOutageCreated(t, client, "Sippy", "sippy", 30*time.Second)
			require.NotNil(t, outageBefore, "Component should be monitored before removal")

			// Cleanup the outage
			resp, err = http.Get(mockMonitoredComponentURL + "/up")
			require.NoError(t, err)
			resp.Body.Close()
			waitForOutageResolved(t, client, "Sippy", "sippy", outageBefore.ID, 30*time.Second)
			cleanupActiveOutages(t, client, "Sippy", "sippy")

			// Remove the component from config
			updateComponentMonitorConfig(t, func(config *types.ComponentMonitorConfig) {
				// Filter out sippy/sippy component
				filtered := []types.MonitoringComponent{}
				for _, comp := range config.Components {
					if comp.ComponentSlug == "sippy" && comp.SubComponentSlug == "sippy" {
						continue // Skip this component
					}
					filtered = append(filtered, comp)
				}
				config.Components = filtered
			})

			// Bring the service down
			resp, err = http.Get(mockMonitoredComponentURL + "/down")
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, resp.StatusCode)
			resp.Body.Close()

			// Wait 15 seconds to ensure the orchestrator has had time to probe if it were still monitoring
			// If the orchestrator is still monitoring, it would create an outage during this time
			time.Sleep(15 * time.Second)

			// Verify no outage was created (component should not be monitored)
			outages := getOutages(t, client, "Sippy", "sippy")
			activeOutages := filterActiveOutages(outages)
			assert.Empty(t, activeOutages, "Component should not be monitored after removal from config")

			// Cleanup - bring service back up
			resp, err = http.Get(mockMonitoredComponentURL + "/up")
			require.NoError(t, err)
			resp.Body.Close()
		})
	}
}
