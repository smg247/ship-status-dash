package types

import (
	"encoding/json"

	"gopkg.in/yaml.v3"

	"ship-status-dash/pkg/utils"
)

// DashboardConfig contains the dashboardapplication configuration including component definitions.
type DashboardConfig struct {
	Components        []*Component    `json:"components" yaml:"components"`
	Tags              []Tag           `json:"tags" yaml:"tags"`
	TrustedDelegators []string        `json:"trusted_delegators,omitempty" yaml:"trusted_delegators,omitempty"`
	TeamSLOs          []TeamSLOConfig `json:"team_slos,omitempty" yaml:"team_slos,omitempty"`
}

// AssignSlugs sets component and sub-component slugs from their names.
// Call this before ValidateTeamSLOs so slo_component lookups use Slug.
func (c *DashboardConfig) AssignSlugs() {
	for _, component := range c.Components {
		component.Slug = utils.Slugify(component.Name)
		for i := range component.Subcomponents {
			component.Subcomponents[i].Slug = utils.Slugify(component.Subcomponents[i].Name)
		}
	}
}

func (c *DashboardConfig) GetComponentBySlug(slug string) *Component {
	for i := range c.Components {
		if c.Components[i].Slug == slug {
			return c.Components[i]
		}
	}
	return nil
}

// SubComponentRef identifies a sub-component by config slugs for server-side filtering.
type SubComponentRef struct {
	ComponentSlug string
	SubSlug       string
}

// SubComponentRefsMatching returns component and sub-component slugs that satisfy the optional filters.
// Filters use AND semantics consistent with the sub-components list API: componentSlug, tag, and team
// narrow results; when subSlug is non-empty, only that sub-component is included (if it passes other filters).
func (c *DashboardConfig) SubComponentRefsMatching(componentSlug, subSlug, tag, team string) []SubComponentRef {
	var refs []SubComponentRef
	for _, component := range c.Components {
		if componentSlug != "" && component.Slug != componentSlug {
			continue
		}
		if team != "" && team != component.ShipTeam {
			continue
		}
		for i := range component.Subcomponents {
			sub := &component.Subcomponents[i]
			if subSlug != "" && sub.Slug != subSlug {
				continue
			}
			if tag != "" {
				var match bool
				for _, t := range sub.Tags {
					if t == tag {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			refs = append(refs, SubComponentRef{ComponentSlug: component.Slug, SubSlug: sub.Slug})
		}
	}
	return refs
}

// Component represents a top-level system component with sub-components and ownership information.
type Component struct {
	Name           string                 `json:"name" yaml:"name"`
	Slug           string                 `json:"slug"`
	Description    string                 `json:"description" yaml:"description"`
	ShipTeam       string                 `json:"ship_team" yaml:"ship_team"`
	SlackReporting []SlackReportingConfig `json:"slack_reporting,omitempty" yaml:"slack_reporting,omitempty"`
	Subcomponents  []SubComponent         `json:"sub_components" yaml:"sub_components"`
	Owners         []Owner                `json:"owners" yaml:"owners"`
	// SLOComponent hides this component from home and team list APIs.
	// SLO well membership is team_slos[].slo_components, not ship_team.
	SLOComponent bool `json:"slo_component,omitempty" yaml:"slo_component,omitempty"`
}

func (c *Component) GetSubComponentBySlug(slug string) *SubComponent {
	for i := range c.Subcomponents {
		if c.Subcomponents[i].Slug == slug {
			return &c.Subcomponents[i]
		}
	}
	return nil
}

// SubComponent represents a sub-component that can have outages tracked against it.
type SubComponent struct {
	Name                 string      `json:"name" yaml:"name"`
	Slug                 string      `json:"slug"`
	Description          string      `json:"description" yaml:"description"`
	LongDescription      string      `json:"long_description,omitempty" yaml:"long_description,omitempty"`
	DocumentationURL     string      `json:"documentation_url,omitempty" yaml:"documentation_url,omitempty"`
	Tags                 []string    `json:"tags,omitempty" yaml:"tags,omitempty"`
	Monitoring           *Monitoring `json:"monitoring,omitempty" yaml:"monitoring,omitempty"`
	RequiresConfirmation bool        `json:"requires_confirmation" yaml:"requires_confirmation"`
	// Critical indicates that an outage on this sub-component should propagate its severity
	// to the parent component status, bypassing the generic "partial" roll-up.
	Critical bool `json:"critical,omitempty" yaml:"critical,omitempty"`
	// ExcludeFromMainOutageWell, when true, keeps this sub-component out of the home-page
	// "In Outage" well and does not light the ship-logo fire.
	ExcludeFromMainOutageWell bool                   `json:"exclude_from_main_outage_well,omitempty" yaml:"exclude_from_main_outage_well,omitempty"`
	SlackReporting            []SlackReportingConfig `json:"slack_reporting,omitempty" yaml:"slack_reporting,omitempty"`
	// ReportThreshold is the number of community reports required to upgrade a suspected outage
	// to degraded and trigger Slack notifications. Defaults to 3 when unset.
	ReportThreshold int `json:"report_threshold,omitempty" yaml:"report_threshold,omitempty"`
}

const DefaultReportThreshold = 3

// Monitoring defines how this sub-component is automatically monitored.
type Monitoring struct {
	Frequency string `json:"frequency" yaml:"frequency"`
	// ComponentMonitor is the name of the component monitor that will be used to report the status of this sub-component
	// It must match the component monitor name in the report request
	ComponentMonitor string `json:"component_monitor" yaml:"component_monitor"`
	// AutoResolve is a flag that indicates whether outages discovered by the component-monitor should be automatically resolved when
	// the component-monitor reports the sub-component is healthy.
	AutoResolve bool `json:"auto_resolve" yaml:"auto_resolve"`
	// OutagePerReason, when true, treats each incoming probe Reason (Type+Check) as its own outage
	// instead of one outage per sub-component. Extra active outages whose reasons left the set are
	// auto-resolved when AutoResolve is true.
	OutagePerReason bool `json:"outage_per_reason,omitempty" yaml:"outage_per_reason,omitempty"`
}

// Owner represents ownership information for a component, either via Rover group or service account.
type Owner struct {
	RoverGroup string `json:"rover_group,omitempty" yaml:"rover_group,omitempty"`
	// ServiceAccount owners are used for component-monitor status reporting.
	// In order to report the status of a sub-component, the service account must be an owner of the component.
	ServiceAccount string `json:"service_account,omitempty" yaml:"service_account,omitempty"`
	// User is a username of a user who is an admin of the component, this is used for development/testing purposes only
	User string `json:"user,omitempty" yaml:"user,omitempty"`
}

// TeamSLOConfig is the team-scoped SLO definition from dashboard YAML.
type TeamSLOConfig struct {
	Team   string  `json:"team" yaml:"team"`
	Owners []Owner `json:"owners" yaml:"owners"`
	// SLOComponents are component slugs whose active outages appear in this team's SLO wells.
	// Each slug must match a component with slo_component set.
	SLOComponents []string   `json:"slo_components,omitempty" yaml:"slo_components,omitempty"`
	SLOs          []NamedSLO `json:"slos" yaml:"slos"`
}

// NamedSLO is one objective. At most one SLO per team may set Workspace.
type NamedSLO struct {
	Name        string        `json:"name" yaml:"name"`
	DisplayName string        `json:"display_name" yaml:"display_name"`
	Source      string        `json:"source" yaml:"source"`
	Workspace   *SLOWorkspace `json:"workspace,omitempty" yaml:"workspace,omitempty"`
}

// SLOWorkspace is the versioned document contract shared with producers.
// Spec holds the schema-specific settings. Generic code does not interpret it.
type SLOWorkspace struct {
	Kind          string          `json:"kind" yaml:"kind"`
	SchemaVersion int             `json:"schema_version" yaml:"schema_version"`
	Spec          json.RawMessage `json:"spec,omitempty" yaml:"-"`
}

// UnmarshalYAML keeps kind and schema_version and stores every other field in Spec.
func (w *SLOWorkspace) UnmarshalYAML(value *yaml.Node) error {
	var raw map[string]any
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if kind, ok := raw["kind"].(string); ok {
		w.Kind = kind
	}
	switch version := raw["schema_version"].(type) {
	case int:
		w.SchemaVersion = version
	case int64:
		w.SchemaVersion = int(version)
	case uint64:
		w.SchemaVersion = int(version)
	}
	delete(raw, "kind")
	delete(raw, "schema_version")
	if len(raw) == 0 {
		w.Spec = nil
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	w.Spec = encoded
	return nil
}

// MarshalYAML writes kind, schema_version, and the spec fields.
// A config reload round-trip has to keep those fields or validation rejects the file.
func (w SLOWorkspace) MarshalYAML() (any, error) {
	out := map[string]any{
		"kind":           w.Kind,
		"schema_version": w.SchemaVersion,
	}
	if len(w.Spec) == 0 {
		return out, nil
	}
	var extra map[string]any
	if err := json.Unmarshal(w.Spec, &extra); err != nil {
		return nil, err
	}
	for key, value := range extra {
		out[key] = value
	}
	return out, nil
}

// TeamSLOByTeam returns the SLO config for a team, or nil.
func (c *DashboardConfig) TeamSLOByTeam(team string) *TeamSLOConfig {
	for i := range c.TeamSLOs {
		if c.TeamSLOs[i].Team == team {
			return &c.TeamSLOs[i]
		}
	}
	return nil
}

// Workspace returns the single workspace on this team, or nil.
func (t *TeamSLOConfig) Workspace() *SLOWorkspace {
	for i := range t.SLOs {
		if t.SLOs[i].Workspace != nil {
			return t.SLOs[i].Workspace
		}
	}
	return nil
}

// ComponentMonitorConfig contains the configuration for the component monitor.
type ComponentMonitorConfig struct {
	Components []MonitoringComponent `json:"components" yaml:"components"`
	// Frequency is the orchestrator tick and the default cadence for component entries
	// that do not set their own frequency.
	Frequency string `json:"frequency" yaml:"frequency"`
}

// MonitoringComponent contains the configuration for a sub-component monitor in the component monitor.
type MonitoringComponent struct {
	ComponentSlug    string `json:"component_slug" yaml:"component_slug"`
	SubComponentSlug string `json:"sub_component_slug" yaml:"sub_component_slug"`
	// Frequency overrides the instance-level probe cadence for every monitor on this entry.
	// If empty, the instance frequency is used. Must be a duration >= the instance frequency.
	Frequency string `json:"frequency,omitempty" yaml:"frequency,omitempty"`
	// PrometheusMonitors is the configuration for the Prometheus monitor
	PrometheusMonitor *PrometheusMonitor `json:"prometheus_monitor" yaml:"prometheus_monitor"`
	// HTTPMonitor is the configuration for the HTTP monitor
	HTTPMonitor *HTTPMonitor `json:"http_monitor,omitempty" yaml:"http_monitor,omitempty"`
	// SystemdMonitor is the configuration for the systemd unit monitor
	SystemdMonitor *SystemdMonitor `json:"systemd_monitor,omitempty" yaml:"systemd_monitor,omitempty"`
	// JUnitMonitor configures Prow GCS JUnit probing when set (optional).
	JUnitMonitor *JUnitMonitor `json:"junit_monitor,omitempty" yaml:"junit_monitor,omitempty"`
	// JiraMonitor configures Jira JQL probing when set (optional).
	JiraMonitor *JiraMonitor `json:"jira_monitor,omitempty" yaml:"jira_monitor,omitempty"`
}

// JiraMonitor configures searching Jira for issues that should surface as outages.
type JiraMonitor struct {
	// URL is the Jira Cloud base URL (e.g. https://redhat.atlassian.net).
	URL string `json:"url" yaml:"url"`
	// JQL is the query for currently open issues. Required.
	JQL string `json:"jql" yaml:"jql"`
	// Severity is the severity of the outage created for each matching issue. Defaults to Degraded.
	Severity Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
}

type PrometheusMonitor struct {
	PrometheusLocation PrometheusLocation `json:"prometheus_location" yaml:"prometheus_location"`
	// Queries is the list of Prometheus queries to perform
	Queries []PrometheusQuery `json:"queries" yaml:"queries"`
}

// PrometheusLocation specifies how to connect to a Prometheus instance.
// Either url (for e2e/local-dev) or cluster+namespace+route (for production) must be set, but not both.
type PrometheusLocation struct {
	// URL is the direct URL to Prometheus (for e2e and local development).
	// Mutually exclusive with cluster, namespace, service, and route.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// Cluster is the cluster name.
	// When set, namespace must also be set. For in-cluster, service is required; otherwise route is required.
	Cluster string `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	// Namespace is the namespace where the Prometheus route or service exists.
	// Required when cluster is set.
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// Route is the name of the OpenShift Route to Prometheus.
	// Required when cluster is set and not in-cluster.
	Route string `json:"route,omitempty" yaml:"route,omitempty"`
	// Service is the Kubernetes service name for Prometheus. Used when cluster is in-cluster to connect via in-cluster DNS.
	// Required when cluster is in-cluster.
	Service string `json:"service,omitempty" yaml:"service,omitempty"`
}

type PrometheusQuery struct {
	// Query is the Prometheus query to perform
	Query string `json:"query" yaml:"query"`
	// Severity is the severity of the outage that will be created if the query returns no results.
	// If not provided, the severity will default to Down.
	Severity Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
	// FailureQuery is the, optional, Prometheus (instant) query that runs when the Query returns no results
	// It can be used to provide more information as to the reason for the resulting Outage
	FailureQuery string `json:"failure_query,omitempty" yaml:"failure_query,omitempty"`
	// Duration is the duration to use in a range query.
	// If provided, the query will be a range query.
	// If not provided, the query will be an instant query.
	Duration string `json:"duration" yaml:"duration"`
	// Step is the resolution (time between data points) for range queries.
	// If not provided, a default step will be calculated based on the duration.
	// If provided, it must be a valid duration string (e.g., "15s", "1m").
	Step string `json:"step" yaml:"step"`
}

type SystemdMonitor struct {
	// Unit is the systemd unit name to monitor (e.g., "my-service.service")
	Unit string `json:"unit" yaml:"unit"`
	// Severity is the severity of the outage that will be created if the unit is not active.
	// If not provided, the severity will default to Down.
	Severity Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
}

const (
	// JUnitArtifactStyleGCS uses the public GCS object URL.
	JUnitArtifactStyleGCS = "gcs"
	// JUnitArtifactStyleGCSWeb uses the GCSweb proxy (see JUnitDefaultGCSWebBase).
	JUnitArtifactStyleGCSWeb = "gcsweb"
)

// JUnitDefaultGCSWebBase is the default GCSweb host for artifact links (aligns with openshift/release ship-status and job report URLs).
const JUnitDefaultGCSWebBase = "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com"

// JUnitDefaultProwSpyglassBase is the Prow spyglass view host for build log links in JUnit probe reasons.
const JUnitDefaultProwSpyglassBase = "https://prow.ci.openshift.org"

// JUnitMonitor configures reading JUnit XML that Prow uploads to GCS for a job (under logs/<job_name>/,
// e.g. artifacts/junit_canary.xml via latest-build.txt and started.json for staleness—see component-monitor docs).
//
// With history_runs 1 (default), only that latest build is evaluated for pass/fail.
// With history_runs N > 1 and failed_runs_threshold Y, the monitor evaluates up to N recent builds and
// reports unhealthy only when at least Y of those runs share the same failure pattern (the same set of
// failed testcase names, or runs all bucketed as zero JUnit tests)—not merely “any Y failing runs out of N,”
// which avoids noisy alerts from unrelated flake patterns.
type JUnitMonitor struct {
	// GCSBucket is the Prow GCS bucket name. If not provided, test-platform-results-public is used.
	GCSBucket string `json:"gcs_bucket,omitempty" yaml:"gcs_bucket,omitempty"`
	// JobName is the Prow job name (under logs/ in the bucket).
	JobName string `json:"job_name" yaml:"job_name"`
	// MaxAge is the maximum age of the build referenced by latest-build.txt before the probe reports unhealthy. Must be a valid Go duration.
	MaxAge string `json:"max_age" yaml:"max_age"`
	// Severity is the severity to report on failure or a stale build. If not provided, the severity will default to Degraded.
	Severity Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
	// ArtifactURLStyle selects gcs (direct GCS URL) or gcsweb (GCSweb proxy URL). If not provided, gcs is used.
	ArtifactURLStyle string `json:"artifact_url_style,omitempty" yaml:"artifact_url_style,omitempty"`
	// GCSWebBaseURL is the GCSweb origin to use when ArtifactURLStyle is gcsweb. If not provided, JUnitDefaultGCSWebBase is used.
	GCSWebBaseURL string `json:"gcsweb_base_url,omitempty" yaml:"gcsweb_base_url,omitempty"`
	// HistoryRuns is the number of recent Prow build IDs to evaluate. If 0, 1 is used. When greater than 1, FailedRunsThreshold and GCS list responses apply; staleness (MaxAge) still uses only the latest build from latest-build.txt.
	HistoryRuns int `json:"history_runs,omitempty" yaml:"history_runs,omitempty"`
	// FailedRunsThreshold (Y) is used when HistoryRuns (N) is greater than 1. Unhealthy if the *largest* group
	// of runs that share the same failure pattern has size at least Y. A pattern is the sorted set of failed
	// testcase names, or a shared bucket for zero total JUnit tests. It is not "Y arbitrary red runs in N."
	// Y must be between 1 and N (inclusive). When HistoryRuns is 1, the field is ignored.
	FailedRunsThreshold int `json:"failed_runs_threshold,omitempty" yaml:"failed_runs_threshold,omitempty"`
}

type HTTPMonitor struct {
	// URL is the URL to probe
	URL string `json:"url" yaml:"url"`
	// Code is the expected HTTP status code
	Code int `json:"code" yaml:"code"`
	// RetryAfter is the duration to wait before retrying the probe only when the status code is not as expected
	RetryAfter string `json:"retry_after" yaml:"retry_after"`
	// Severity is the severity of the outage that will be created if the HTTP request fails.
	// If not provided, the severity will default to Down.
	Severity Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
}

// SlackReportingConfig defines Slack reporting configuration for a channel with optional severity threshold.
type SlackReportingConfig struct {
	Channel  string    `json:"channel" yaml:"channel"`
	Severity *Severity `json:"severity,omitempty" yaml:"severity,omitempty"`
}

// GetSlackReporting returns the Slack reporting configuration for a sub-component.
// If the sub-component has its own SlackReporting config, it is returned.
// Otherwise, the component's SlackReporting config is returned.
func GetSlackReporting(component *Component, subComponent *SubComponent) []SlackReportingConfig {
	if subComponent != nil && len(subComponent.SlackReporting) > 0 {
		return subComponent.SlackReporting
	}
	if component != nil && len(component.SlackReporting) > 0 {
		return component.SlackReporting
	}
	return nil
}

type Tag struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Color       string `json:"color" yaml:"color"`
}
