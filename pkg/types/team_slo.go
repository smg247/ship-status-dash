package types

import (
	"fmt"
	"strings"
)

// ValidateTeamSLOs checks team SLO YAML. Unknown evaluator sources are allowed
// and ignored at read time. Callers must also reject unknown workspace schemas.
func (c *DashboardConfig) ValidateTeamSLOs() error {
	if c == nil {
		return nil
	}
	seenTeams := map[string]bool{}
	listedSLOComponents := map[string]string{}
	for i := range c.TeamSLOs {
		team := &c.TeamSLOs[i]
		name := strings.TrimSpace(team.Team)
		if name == "" {
			return fmt.Errorf("team_slos[%d]: team is required", i)
		}
		if seenTeams[name] {
			return fmt.Errorf("team_slos: duplicate team %q", name)
		}
		seenTeams[name] = true
		if len(team.Owners) == 0 {
			return fmt.Errorf("team_slos %q: owners is required", name)
		}
		if !ownersPresent(team.Owners) {
			return fmt.Errorf("team_slos %q: owners must include a user, service account, or rover group", name)
		}
		workspaces := 0
		seenSLOComponent := map[string]bool{}
		for _, raw := range team.SLOComponents {
			slug := strings.TrimSpace(raw)
			if slug == "" {
				return fmt.Errorf("team_slos %q: slo_component slug is required", name)
			}
			if seenSLOComponent[slug] {
				return fmt.Errorf("team_slos %q: duplicate slo_component %q", name, slug)
			}
			seenSLOComponent[slug] = true
			if listedOn, ok := listedSLOComponents[slug]; ok {
				return fmt.Errorf("slo_component %q is listed on both %q and %q", slug, listedOn, name)
			}
			listedSLOComponents[slug] = name
			component := c.GetComponentBySlug(slug)
			if component == nil {
				return fmt.Errorf("team_slos %q: slo_component %q does not exist", name, slug)
			}
			if !component.SLOComponent {
				return fmt.Errorf("team_slos %q: component %q is not marked slo_component", name, slug)
			}
		}
		seenSLO := map[string]bool{}
		for j := range team.SLOs {
			slo := &team.SLOs[j]
			if strings.TrimSpace(slo.Name) == "" {
				return fmt.Errorf("team_slos %q: slo name is required", name)
			}
			if seenSLO[slo.Name] {
				return fmt.Errorf("team_slos %q: duplicate slo %q", name, slo.Name)
			}
			seenSLO[slo.Name] = true
			if strings.TrimSpace(slo.Source) == "" {
				return fmt.Errorf("team_slos %q slo %q: source is required", name, slo.Name)
			}
			if slo.Workspace == nil {
				continue
			}
			workspaces++
			if workspaces > 1 {
				return fmt.Errorf("team_slos %q: only one workspace is allowed", name)
			}
			if err := validateWorkspace(name, slo.Workspace); err != nil {
				return err
			}
		}
	}
	for _, component := range c.Components {
		if component == nil || !component.SLOComponent {
			continue
		}
		slug := component.EffectiveSlug()
		if _, ok := listedSLOComponents[slug]; !ok {
			return fmt.Errorf("component %q is marked slo_component but is not listed in team_slos", slug)
		}
	}
	return nil
}

// NormalizeTeamSLOs applies defaults after validation.
func (c *DashboardConfig) NormalizeTeamSLOs() {
	if c == nil {
		return
	}
	for i := range c.TeamSLOs {
		for j, slug := range c.TeamSLOs[i].SLOComponents {
			c.TeamSLOs[i].SLOComponents[j] = strings.TrimSpace(slug)
		}
		for j := range c.TeamSLOs[i].SLOs {
			ws := c.TeamSLOs[i].SLOs[j].Workspace
			if ws != nil && ws.RecentPayloads <= 0 {
				ws.RecentPayloads = 5
			}
		}
	}
}

func validateWorkspace(team string, ws *SLOWorkspace) error {
	if strings.TrimSpace(ws.Kind) == "" {
		return fmt.Errorf("team_slos %q: workspace.kind is required", team)
	}
	if ws.SchemaVersion <= 0 {
		return fmt.Errorf("team_slos %q: workspace.schema_version is required", team)
	}
	if ws.RecentPayloads < 0 {
		return fmt.Errorf("team_slos %q: recent_payloads must be positive", team)
	}
	seen := map[string]bool{}
	for _, stream := range ws.Streams {
		if strings.TrimSpace(stream.Name) == "" {
			return fmt.Errorf("team_slos %q: stream name is required", team)
		}
		if seen[stream.Name] {
			return fmt.Errorf("team_slos %q: duplicate stream %q", team, stream.Name)
		}
		seen[stream.Name] = true
	}
	return nil
}

func ownersPresent(owners []Owner) bool {
	for _, owner := range owners {
		if strings.TrimSpace(owner.User) != "" || strings.TrimSpace(owner.ServiceAccount) != "" || strings.TrimSpace(owner.RoverGroup) != "" {
			return true
		}
	}
	return false
}
