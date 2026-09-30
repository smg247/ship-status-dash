package slo

import (
	"sort"
	"time"

	"ship-status-dash/pkg/types"
)

const (
	SourcePayloadAcceptance = "payload_acceptance"
	payloadWindow           = 24 * time.Hour
	payloadMinAccepted      = 1
	defaultRecentPayloads   = 5
)

// IsTRTPayloadWorkspace reports the payload_streams v1 document TRT uses.
func IsTRTPayloadWorkspace(ws *types.SLOWorkspace) bool {
	return ws != nil && ws.Kind == KindPayloadStreams && ws.SchemaVersion == SchemaVersionV1
}

func evaluateTRTPayloadAcceptance(now time.Time, named types.NamedSLO, items []types.SLOWorkspaceItem) Evaluation {
	cutoff := now.Add(-payloadWindow)
	ev := Evaluation{
		Name:        named.Name,
		DisplayName: named.DisplayName,
		Source:      named.Source,
		Window:      "24h",
		Target:      Target{MinAccepted: payloadMinAccepted},
		Met:         true,
		Groups:      []GroupEval{},
	}
	for _, stream := range named.Workspace.Streams {
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
		group.Met = group.Accepted >= payloadMinAccepted
		if !group.Met {
			ev.Met = false
		}
		ev.Groups = append(ev.Groups, group)
	}
	if len(named.Workspace.Streams) == 0 {
		ev.Met = false
	}
	return ev
}

// TRTPayloadPruneIDs returns item IDs that are outside the 24h window and outside the
// last-N display set. The newest Accepted row for each watched stream is kept.
// Rows for streams no longer in the watched list follow the window only.
func TRTPayloadPruneIDs(now time.Time, streams []string, recent int, items []types.SLOWorkspaceItem) []uint {
	if recent <= 0 {
		recent = defaultRecentPayloads
	}
	watched := make(map[string]bool, len(streams))
	for _, name := range streams {
		watched[name] = true
	}
	cutoff := now.Add(-payloadWindow)
	byStream := map[string][]types.SLOWorkspaceItem{}
	var other []types.SLOWorkspaceItem
	for _, item := range items {
		if watched[item.GroupKey] {
			byStream[item.GroupKey] = append(byStream[item.GroupKey], item)
			continue
		}
		other = append(other, item)
	}

	var drop []uint
	for _, name := range streams {
		drop = append(drop, trtPayloadPruneStream(cutoff, recent, byStream[name])...)
	}
	for _, item := range other {
		if item.OccurredAt.Before(cutoff) {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

func trtPayloadPruneStream(cutoff time.Time, recent int, items []types.SLOWorkspaceItem) []uint {
	if len(items) == 0 {
		return nil
	}
	sorted := append([]types.SLOWorkspaceItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].OccurredAt.Equal(sorted[j].OccurredAt) {
			return sorted[i].ID > sorted[j].ID
		}
		return sorted[i].OccurredAt.After(sorted[j].OccurredAt)
	})
	keep := map[uint]bool{}
	for i, item := range sorted {
		if i < recent || !item.OccurredAt.Before(cutoff) {
			keep[item.ID] = true
		}
	}
	for _, item := range sorted {
		if item.Outcome == "Accepted" {
			keep[item.ID] = true
			break
		}
	}
	var drop []uint
	for _, item := range items {
		if !keep[item.ID] {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

// TRTPayloadDisplayItems returns the newest recent items per watched stream, newest first within each stream.
func TRTPayloadDisplayItems(streams []string, recent int, items []types.SLOWorkspaceItem) []types.SLOWorkspaceItem {
	if recent <= 0 {
		recent = defaultRecentPayloads
	}
	byStream := map[string][]types.SLOWorkspaceItem{}
	for _, item := range items {
		byStream[item.GroupKey] = append(byStream[item.GroupKey], item)
	}
	var out []types.SLOWorkspaceItem
	for _, name := range streams {
		group := byStream[name]
		sort.Slice(group, func(i, j int) bool {
			if group[i].OccurredAt.Equal(group[j].OccurredAt) {
				return group[i].ID > group[j].ID
			}
			return group[i].OccurredAt.After(group[j].OccurredAt)
		})
		if len(group) > recent {
			group = group[:recent]
		}
		out = append(out, group...)
	}
	if out == nil {
		out = []types.SLOWorkspaceItem{}
	}
	return out
}

// TRTPayloadItemKeys returns every stored item key for the watched streams.
func TRTPayloadItemKeys(streams []string, items []types.SLOWorkspaceItem) []string {
	watched := make(map[string]bool, len(streams))
	for _, name := range streams {
		watched[name] = true
	}
	var keys []string
	for _, item := range items {
		if watched[item.GroupKey] {
			keys = append(keys, item.ItemKey)
		}
	}
	if keys == nil {
		keys = []string{}
	}
	return keys
}
