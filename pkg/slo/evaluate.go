package slo

import (
	"time"

	"ship-status-dash/pkg/types"
)

// Evaluation is one named SLO result computed from stored rows.
type Evaluation struct {
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	Source      string      `json:"source"`
	Window      string      `json:"window"`
	Target      Target      `json:"target"`
	Met         bool        `json:"met"`
	Groups      []GroupEval `json:"groups"`
}

// Target is the evaluator's goal, returned so the UI does not hardcode it.
type Target struct {
	MinAccepted int `json:"min_accepted"`
}

// GroupEval is one stream's met/miss result.
type GroupEval struct {
	Key            string     `json:"key"`
	Accepted       int        `json:"accepted"`
	Met            bool       `json:"met"`
	LastAcceptedAt *time.Time `json:"last_accepted_at,omitempty"`
}

// Evaluate returns results for known sources on this team. Unknown sources are omitted.
func Evaluate(now time.Time, team *types.TeamSLOConfig, items []types.SLOWorkspaceItem) []Evaluation {
	if team == nil {
		return []Evaluation{}
	}
	var out []Evaluation
	for _, named := range team.SLOs {
		if named.Source != SourcePayloadAcceptance || !IsTRTPayloadWorkspace(named.Workspace) {
			continue
		}
		out = append(out, evaluateTRTPayloadAcceptance(now, named, items))
	}
	if out == nil {
		out = []Evaluation{}
	}
	return out
}
