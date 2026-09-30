package v1

import (
	"time"

	"ship-status-dash/pkg/types"
)

// Target is this evaluator's goal.
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

// Result is the payload_streams v1 evaluation.
type Result struct {
	Window string      `json:"window"`
	Target Target      `json:"target"`
	Groups []GroupEval `json:"groups"`
	Met    bool        `json:"-"`
}

// Evaluate scores accepted payloads in the configured window.
func Evaluate(now time.Time, named types.NamedSLO, settings Settings, items []types.SLOWorkspaceItem) Result {
	cutoff := now.Add(-settings.WindowDuration())
	result := Result{
		Window: settings.Window,
		Target: Target{MinAccepted: settings.MinAccepted},
		Met:    true,
		Groups: []GroupEval{},
	}
	for _, stream := range settings.Streams {
		group := GroupEval{Key: stream.Name, Met: false}
		var last *time.Time
		for i := range items {
			item := &items[i]
			if item.Kind != named.Workspace.Kind || item.GroupKey != stream.Name || item.Outcome != "Accepted" {
				continue
			}
			occurred := item.OccurredAt.UTC()
			if occurred.After(now) {
				continue
			}
			if last == nil || occurred.After(*last) {
				t := occurred
				last = &t
			}
			if !occurred.Before(cutoff) {
				group.Accepted++
			}
		}
		group.LastAcceptedAt = last
		group.Met = group.Accepted >= settings.MinAccepted
		if !group.Met {
			result.Met = false
		}
		result.Groups = append(result.Groups, group)
	}
	if len(settings.Streams) == 0 {
		result.Met = false
	}
	return result
}
