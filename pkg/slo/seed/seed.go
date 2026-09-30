// Package seed builds the local and e2e payload workspace from dashboard config.
package seed

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
)

const (
	// UpdatedBy is stored on seeded workspace rows and the sample outage.
	UpdatedBy = "seed-slo"
	// JiraURL is the sample Jira link attached to the newest rejected payloads.
	JiraURL = "https://redhat.atlassian.net/browse/TRT-4120"
	// OutageDescription is the sample TRT incident created with the workspace.
	OutageDescription = "aws-ovn-upgrade blocking nightlies"

	upgradeJob = "e2e-aws-ovn-upgrade"
	metalJob   = "e2e-metal-ipi-ovn-serial"

	// PruneCandidateItemKey is one rejected payload on the first stream.
	// PruneCandidateAgo sits a full day past the 24h retention window, so a long
	// e2e run cannot move the row back into the window or into the last-N set
	// while the rest of the seed is present.
	PruneCandidateItemKey = "prune-candidate"
	PruneCandidateAgo     = 48 * time.Hour
)

// Stream roles written by buildSeed. The first stream is a recent reject unless it
// is the miss. The miss stream is the third configured stream, or the last
// stream when fewer than three are configured.
const (
	RoleRecentReject = "recent_reject"
	RoleMet          = "met"
	RoleMiss         = "miss"
)

// metAcceptAgo staggers newest-accepted times for ordinary met streams.
var metAcceptAgo = []time.Duration{4 * time.Hour, 8 * time.Hour, 6 * time.Hour}

type payloadSpec struct {
	ago      time.Duration
	outcome  string
	jobs     []slo.PayloadJobV1
	analysis bool
}

type seededItem struct {
	item  types.SLOWorkspaceItem
	links []types.SLOWorkspaceLink
}

// PayloadWorkspace returns the team and workspace named in config.
func PayloadWorkspace(cfg *types.DashboardConfig) (string, *types.SLOWorkspace, error) {
	if cfg == nil {
		return "", nil, fmt.Errorf("config is required")
	}
	for i := range cfg.TeamSLOs {
		team := &cfg.TeamSLOs[i]
		ws := team.Workspace()
		if ws == nil || ws.Kind != slo.KindPayloadStreams || ws.SchemaVersion != slo.SchemaVersionV1 {
			continue
		}
		if len(ws.Streams) == 0 {
			return "", nil, fmt.Errorf("team %s payload workspace has no streams", team.Team)
		}
		return team.Team, ws, nil
	}
	return "", nil, fmt.Errorf("no payload_streams v1 workspace in config")
}

// buildSeed returns the payload rows for the configured streams.
func buildSeed(now time.Time, team string, streams []types.SLOStream, recent int) ([]seededItem, error) {
	now = now.UTC()
	if recent <= 0 {
		recent = 5
	}
	metN := 0
	var out []seededItem
	for i, stream := range streams {
		var specs []payloadSpec
		switch RoleAt(len(streams), i) {
		case RoleMiss:
			specs = missSpecs(recent)
		case RoleRecentReject:
			specs = recentRejectSpecs(recent)
		default:
			specs = metSpecs(recent, metAcceptAgo[metN%len(metAcceptAgo)])
			metN++
		}
		for _, spec := range specs {
			item, err := itemFor(now, team, stream, spec)
			if err != nil {
				return nil, err
			}
			out = append(out, seededItem{item: item})
		}
	}
	if len(streams) > 0 {
		item, err := itemFor(now, team, streams[0], payloadSpec{ago: PruneCandidateAgo, outcome: "Rejected"})
		if err != nil {
			return nil, err
		}
		item.ItemKey = PruneCandidateItemKey
		out = append(out, seededItem{item: item})
	}
	return out, nil
}

// MissIndex is the configured stream that misses the SLO. The third stream
// matches the current local list. Shorter lists use the last stream.
func MissIndex(n int) int {
	if n >= 3 {
		return 2
	}
	if n >= 2 {
		return n - 1
	}
	return -1
}

func recentRejectSpecs(n int) []payloadSpec {
	upgrade := failedJob(upgradeJob, "Same disruption as TRT-4120. Not infra.")
	metal := failedJob(metalJob, "Likely flake. Watching next payload.")
	specs := []payloadSpec{
		{ago: 2 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade, metal}, analysis: true},
		{ago: 6 * time.Hour, outcome: "Accepted"},
		{ago: 12 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade}, analysis: true},
		{ago: 16 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{metal}, analysis: true},
		{ago: 20 * time.Hour, outcome: "Accepted"},
	}
	return applyStreaks(fitSpecs(specs, n, 2))
}

func metSpecs(n int, acceptAgo time.Duration) []payloadSpec {
	upgrade := failedJob(upgradeJob, "Same disruption as TRT-4120. Not infra.")
	metal := failedJob(metalJob, "Likely flake. Watching next payload.")
	specs := []payloadSpec{
		{ago: acceptAgo, outcome: "Accepted"},
		{ago: acceptAgo + 6*time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade, metal}, analysis: true},
		{ago: acceptAgo + 10*time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade}, analysis: true},
		{ago: acceptAgo + 14*time.Hour, outcome: "Accepted"},
		{ago: acceptAgo + 16*time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{metal}, analysis: true},
	}
	return applyStreaks(fitSpecs(specs, n, 1))
}

func missSpecs(n int) []payloadSpec {
	if n < 1 {
		n = 1
	}
	upgrade := failedJob(upgradeJob, "Backport candidate. Waiting on 5.1 fix.")
	metal := failedJob(metalJob, "Likely flake. Watching next payload.")
	rejects := []payloadSpec{
		{ago: 3 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade}, analysis: true},
		{ago: 9 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{upgrade}, analysis: true},
		{ago: 15 * time.Hour, outcome: "Rejected"},
		{ago: 21 * time.Hour, outcome: "Rejected", jobs: []slo.PayloadJobV1{metal}, analysis: true},
	}
	for len(rejects) < n-1 {
		rejects = append(rejects, payloadSpec{
			ago:     time.Duration(3+6*len(rejects)) * time.Hour,
			outcome: "Rejected",
		})
	}
	if n == 1 {
		return []payloadSpec{{ago: 32 * time.Hour, outcome: "Accepted"}}
	}
	out := append([]payloadSpec{}, rejects[:n-1]...)
	out = append(out, payloadSpec{ago: 32 * time.Hour, outcome: "Accepted"})
	return applyStreaks(out)
}

// fitSpecs keeps the leading rows that carry the SLO shape, then pads so each
// stream fills the configured recent_payloads list.
func fitSpecs(specs []payloadSpec, n, keep int) []payloadSpec {
	if n < keep {
		n = keep
	}
	for len(specs) < n {
		specs = append(specs, payloadSpec{
			ago:     specs[len(specs)-1].ago + 4*time.Hour,
			outcome: "Rejected",
		})
	}
	return specs[:n]
}

func itemFor(now time.Time, team string, stream types.SLOStream, spec payloadSpec) (types.SLOWorkspaceItem, error) {
	occurred := now.Add(-spec.ago).UTC()
	name := stream.Name
	tag := name + "-" + occurred.Format("2006-01-02-150405")
	jobs := spec.jobs
	if jobs == nil {
		jobs = []slo.PayloadJobV1{}
	}
	controller := strings.TrimSpace(stream.Controller)
	if controller == "" {
		controller = "amd64"
	}
	doc := slo.PayloadDetailsV1{
		PayloadURL: fmt.Sprintf("https://%s.ocp.releases.ci.openshift.org/releasestream/%s/release/%s", controller, name, tag),
		Jobs:       jobs,
	}
	if spec.analysis {
		doc.AnalysisURL = fmt.Sprintf("https://storage.googleapis.com/test-platform-results-public/payload-agent/%s.html", tag)
	}
	details, err := json.Marshal(doc)
	if err != nil {
		return types.SLOWorkspaceItem{}, err
	}
	if err := slo.ValidateDetails(slo.KindPayloadStreams, slo.SchemaVersionV1, details); err != nil {
		return types.SLOWorkspaceItem{}, fmt.Errorf("seed %s: %w", tag, err)
	}
	return types.SLOWorkspaceItem{
		Team:          team,
		Kind:          slo.KindPayloadStreams,
		SchemaVersion: slo.SchemaVersionV1,
		ItemKey:       tag,
		GroupKey:      name,
		OccurredAt:    occurred,
		Outcome:       spec.outcome,
		Details:       details,
		UpdatedBy:     UpdatedBy,
	}, nil
}

// RoleAt reports which payload pattern buildSeed writes for stream index i.
func RoleAt(streamCount, index int) string {
	switch {
	case index == MissIndex(streamCount):
		return RoleMiss
	case index == 0:
		return RoleRecentReject
	default:
		return RoleMet
	}
}

// JiraStreamIndexes are the streams that receive a sample Jira link.
// The first of these also receives the sample outage link.
func JiraStreamIndexes(streamCount int) []int {
	if streamCount < 1 {
		return nil
	}
	indexes := []int{0}
	if miss := MissIndex(streamCount); miss > 0 {
		indexes = append(indexes, miss)
	}
	return indexes
}

// attachSampleLinks adds a Jira link to the newest rejected payload on the first
// stream and on the miss stream, and an outage link on the first of those.
func attachSampleLinks(seeded []seededItem, streams []types.SLOStream, outageURL string, outageID uint) {
	for n, streamIdx := range JiraStreamIndexes(len(streams)) {
		if streamIdx >= len(streams) {
			continue
		}
		row := newestRejected(seeded, streams[streamIdx].Name)
		if row < 0 {
			continue
		}
		seeded[row].links = append(seeded[row].links, types.SLOWorkspaceLink{
			URL:      JiraURL,
			LinkType: "jira",
		})
		if n == 0 && outageURL != "" && outageID != 0 {
			id := outageID
			seeded[row].links = append(seeded[row].links, types.SLOWorkspaceLink{
				URL:      outageURL,
				LinkType: "outage",
				OutageID: &id,
			})
		}
	}
}

func newestRejected(seeded []seededItem, stream string) int {
	found := -1
	for i := range seeded {
		item := seeded[i].item
		if item.GroupKey != stream || item.Outcome != "Rejected" {
			continue
		}
		if found < 0 || item.OccurredAt.After(seeded[found].item.OccurredAt) {
			found = i
		}
	}
	return found
}

// SLOComponent returns the slug pair for the team's first listed slo_component.
func SLOComponent(cfg *types.DashboardConfig, team string) (string, string, error) {
	if cfg == nil {
		return "", "", fmt.Errorf("config is required")
	}
	teamCfg := cfg.TeamSLOByTeam(team)
	if teamCfg == nil || len(teamCfg.SLOComponents) == 0 {
		return "", "", fmt.Errorf("team %s has no slo component", team)
	}
	component := cfg.GetComponentBySlug(teamCfg.SLOComponents[0])
	if component == nil || !component.SLOComponent || len(component.Subcomponents) == 0 {
		return "", "", fmt.Errorf("team %s slo component %s is missing", team, teamCfg.SLOComponents[0])
	}
	return component.EffectiveSlug(), component.Subcomponents[0].EffectiveSlug(), nil
}

// ReplacePayloads deletes existing payload rows for the team and writes a fresh seed.
func ReplacePayloads(db *gorm.DB, repo repositories.SLOWorkspaceRepository, team string, ws *types.SLOWorkspace, component, sub string) error {
	existing, err := repo.ListByTeam(team)
	if err != nil {
		return err
	}
	var drop []uint
	for _, item := range existing {
		if item.Kind == slo.KindPayloadStreams {
			drop = append(drop, item.ID)
		}
	}
	if err := repo.DeleteItems(drop); err != nil {
		return err
	}

	now := time.Now()
	outageID, outageURL, err := replaceSeedOutage(db, component, sub, now)
	if err != nil {
		return err
	}
	seeded, err := buildSeed(now, team, ws.Streams, ws.RecentPayloads)
	if err != nil {
		return err
	}
	attachSampleLinks(seeded, ws.Streams, outageURL, outageID)
	for i := range seeded {
		stored, err := repo.UpsertItem(&seeded[i].item)
		if err != nil {
			return err
		}
		for j := range seeded[i].links {
			link := seeded[i].links[j]
			link.ItemID = stored.ID
			if _, err := repo.AddLink(&link); err != nil {
				return err
			}
		}
	}
	return nil
}

func replaceSeedOutage(db *gorm.DB, component, sub string, now time.Time) (uint, string, error) {
	repo := repositories.NewGORMOutageRepository(db)
	var existing []types.Outage
	if err := db.Where("component_name = ? AND sub_component_name = ? AND created_by = ?", component, sub, UpdatedBy).Find(&existing).Error; err != nil {
		return 0, "", err
	}
	for i := range existing {
		if err := repo.DeleteOutage(&existing[i], UpdatedBy); err != nil {
			return 0, "", err
		}
	}

	started := now.Add(-14 * time.Hour).UTC()
	outage := &types.Outage{
		ComponentName:    component,
		SubComponentName: sub,
		Severity:         types.SeverityDegraded,
		StartTime:        started,
		Description:      OutageDescription,
		DiscoveredFrom:   UpdatedBy,
		CreatedBy:        UpdatedBy,
		ConfirmedAt:      sql.NullTime{Time: started, Valid: true},
	}
	if err := repo.CreateOutage(outage, UpdatedBy); err != nil {
		return 0, "", err
	}
	if err := repositories.NewGORMOutageLinkRepository(db).AddOutageLink(&types.OutageLink{
		OutageID:    outage.ID,
		URL:         JiraURL,
		LinkType:    types.LinkTypeJira,
		Description: "TRT-4120",
	}); err != nil {
		return 0, "", err
	}
	return outage.ID, fmt.Sprintf("/%s/%s/outages/%d", component, sub, outage.ID), nil
}

// applyStreaks sets recurring_count from consecutive older failures of the same job.
// An accepted payload ends the run. When the newest payload was accepted, no row
// in the stream keeps a streak badge.
func applyStreaks(specs []payloadSpec) []payloadSpec {
	if len(specs) == 0 {
		return specs
	}
	if specs[0].outcome == "Accepted" {
		for i := range specs {
			for j := range specs[i].jobs {
				specs[i].jobs[j].RecurringCount = nil
			}
		}
		return specs
	}
	for i := range specs {
		for j := range specs[i].jobs {
			name := specs[i].jobs[j].Name
			count := 0
			for k := i; k < len(specs); k++ {
				if specs[k].outcome == "Accepted" || !jobFailed(specs[k], name) {
					break
				}
				count++
			}
			if count >= 2 {
				n := count
				specs[i].jobs[j].RecurringCount = &n
				continue
			}
			specs[i].jobs[j].RecurringCount = nil
		}
	}
	return specs
}

func jobFailed(spec payloadSpec, name string) bool {
	for _, job := range spec.jobs {
		if job.Name == name && job.State == "failure" {
			return true
		}
	}
	return false
}

func failedJob(name, notes string) slo.PayloadJobV1 {
	return slo.PayloadJobV1{
		Name:  name,
		URL:   "https://prow.ci.openshift.org/view/gs/test-platform-results-public/logs/" + name,
		State: "failure",
		Notes: notes,
	}
}
