package slo

import (
	"encoding/json"
	"time"

	"ship-status-dash/pkg/types"
)

// ItemView is one workspace row returned by the public team SLO API.
type ItemView struct {
	ID            uint                     `json:"id"`
	Kind          string                   `json:"kind"`
	SchemaVersion int                      `json:"schema_version"`
	ItemKey       string                   `json:"item_key"`
	GroupKey      string                   `json:"group_key"`
	OccurredAt    time.Time                `json:"occurred_at"`
	Outcome       string                   `json:"outcome"`
	Details       json.RawMessage          `json:"details"`
	Notes         string                   `json:"notes"`
	UpdatedBy     string                   `json:"updated_by"`
	Links         []types.SLOWorkspaceLink `json:"links"`
}

// SLOComponentView is one flagged component's active outages.
type SLOComponentView struct {
	Component    string         `json:"component"`
	SubComponent string         `json:"sub_component"`
	Outages      []types.Outage `json:"outages"`
}

// TeamView is GET /api/teams/{team}/slo.
type TeamView struct {
	Team          string              `json:"team"`
	Workspace     *types.SLOWorkspace `json:"workspace,omitempty"`
	Evaluations   []Evaluation        `json:"evaluations"`
	SLOComponents []SLOComponentView  `json:"slo_components"`
	Items         []ItemView          `json:"items"`
	ItemKeys      []string            `json:"item_keys"`
}

// SummaryTeam is one block on the home SLO well.
type SummaryTeam struct {
	Team          string             `json:"team"`
	Evaluations   []Evaluation       `json:"evaluations,omitempty"`
	SLOComponents []SLOComponentView `json:"slo_components,omitempty"`
}

// Summary is GET /api/teams/slo-summary.
type Summary struct {
	Teams []SummaryTeam `json:"teams"`
}

// DropItems returns items whose IDs are not in drop.
func DropItems(items []types.SLOWorkspaceItem, drop []uint) []types.SLOWorkspaceItem {
	if len(drop) == 0 {
		return items
	}
	gone := make(map[uint]bool, len(drop))
	for _, id := range drop {
		gone[id] = true
	}
	out := make([]types.SLOWorkspaceItem, 0, len(items))
	for _, item := range items {
		if !gone[item.ID] {
			out = append(out, item)
		}
	}
	return out
}

// ToItemViews maps stored rows to the public item shape.
func ToItemViews(items []types.SLOWorkspaceItem) []ItemView {
	out := make([]ItemView, 0, len(items))
	for _, item := range items {
		links := item.Links
		if links == nil {
			links = []types.SLOWorkspaceLink{}
		}
		details := json.RawMessage(item.Details)
		if len(details) == 0 {
			details = json.RawMessage(`{}`)
		}
		out = append(out, ItemView{
			ID:            item.ID,
			Kind:          item.Kind,
			SchemaVersion: item.SchemaVersion,
			ItemKey:       item.ItemKey,
			GroupKey:      item.GroupKey,
			OccurredAt:    item.OccurredAt.UTC(),
			Outcome:       item.Outcome,
			Details:       details,
			Notes:         item.Notes,
			UpdatedBy:     item.UpdatedBy,
			Links:         links,
		})
	}
	return out
}

// SLOComponentsForTeam lists components named on the team's slo_components list.
func SLOComponentsForTeam(cfg *types.DashboardConfig, team string, outagesByRef map[types.SubComponentRef][]types.Outage) []SLOComponentView {
	var out []SLOComponentView
	if cfg == nil {
		return []SLOComponentView{}
	}
	teamCfg := cfg.TeamSLOByTeam(team)
	if teamCfg == nil {
		return []SLOComponentView{}
	}
	for _, slug := range teamCfg.SLOComponents {
		component := cfg.GetComponentBySlug(slug)
		if component == nil || !component.SLOComponent {
			continue
		}
		for i := range component.Subcomponents {
			sub := &component.Subcomponents[i]
			ref := types.SubComponentRef{ComponentSlug: component.EffectiveSlug(), SubSlug: sub.EffectiveSlug()}
			outages := outagesByRef[ref]
			if outages == nil {
				outages = []types.Outage{}
			}
			out = append(out, SLOComponentView{
				Component:    component.Name,
				SubComponent: sub.Name,
				Outages:      outages,
			})
		}
	}
	if out == nil {
		out = []SLOComponentView{}
	}
	return out
}

// SummaryTeamNames is the team_slos team list. Incident components appear only when listed there.
func SummaryTeamNames(cfg *types.DashboardConfig) []string {
	if cfg == nil {
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, team := range cfg.TeamSLOs {
		if team.Team == "" || seen[team.Team] {
			continue
		}
		seen[team.Team] = true
		names = append(names, team.Team)
	}
	return names
}
