package slo

import (
	"encoding/json"
	"fmt"
	"time"

	v1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

// Evaluation is one named SLO result. Result is the evaluator-specific document.
type Evaluation struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Source      string          `json:"source"`
	Met         bool            `json:"met"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// Evaluate returns results for known sources on this team. Unknown sources are omitted.
func Evaluate(now time.Time, team *types.TeamSLOConfig, items []types.SLOWorkspaceItem) ([]Evaluation, error) {
	if team == nil {
		return []Evaluation{}, nil
	}
	var out []Evaluation
	for _, named := range team.SLOs {
		ws := named.Workspace
		if ws == nil || named.Source != v1.Source || !KnownWorkspace(ws.Kind, ws.SchemaVersion) {
			continue
		}
		settings, err := v1.ParseSettings(ws.Spec)
		if err != nil {
			return nil, fmt.Errorf("slo %q: %w", named.Name, err)
		}
		result := v1.Evaluate(now, named, settings, items)
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		out = append(out, Evaluation{
			Name:        named.Name,
			DisplayName: named.DisplayName,
			Source:      named.Source,
			Met:         result.Met,
			Result:      raw,
		})
	}
	if out == nil {
		out = []Evaluation{}
	}
	return out, nil
}
