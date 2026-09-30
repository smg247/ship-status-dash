package v1

import (
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"

	"ship-status-dash/pkg/types"
)

// PruneIDs returns item IDs outside the configured window and outside the last-N set.
// The newest Accepted row for each watched stream is kept.
// Rows for streams no longer in the watched list follow the window only.
func PruneIDs(now time.Time, settings Settings, items []types.SLOWorkspaceItem) []uint {
	streams := make([]string, 0, len(settings.Streams))
	for _, stream := range settings.Streams {
		streams = append(streams, stream.Name)
	}
	watched := sets.New[string](streams...)
	cutoff := now.Add(-settings.WindowDuration())
	byStream := map[string][]types.SLOWorkspaceItem{}
	var other []types.SLOWorkspaceItem
	for _, item := range items {
		if watched.Has(item.GroupKey) {
			byStream[item.GroupKey] = append(byStream[item.GroupKey], item)
			continue
		}
		other = append(other, item)
	}

	var drop []uint
	for _, name := range streams {
		drop = append(drop, pruneStream(cutoff, settings.RecentPayloads, byStream[name])...)
	}
	for _, item := range other {
		if item.OccurredAt.Before(cutoff) {
			drop = append(drop, item.ID)
		}
	}
	return drop
}

func pruneStream(cutoff time.Time, recent int, items []types.SLOWorkspaceItem) []uint {
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

// DisplayItems returns the newest recent items per watched stream, newest first within each stream.
func DisplayItems(settings Settings, items []types.SLOWorkspaceItem) []types.SLOWorkspaceItem {
	byStream := map[string][]types.SLOWorkspaceItem{}
	for _, item := range items {
		byStream[item.GroupKey] = append(byStream[item.GroupKey], item)
	}
	var out []types.SLOWorkspaceItem
	for _, stream := range settings.Streams {
		group := byStream[stream.Name]
		sort.Slice(group, func(i, j int) bool {
			if group[i].OccurredAt.Equal(group[j].OccurredAt) {
				return group[i].ID > group[j].ID
			}
			return group[i].OccurredAt.After(group[j].OccurredAt)
		})
		if len(group) > settings.RecentPayloads {
			group = group[:settings.RecentPayloads]
		}
		out = append(out, group...)
	}
	if out == nil {
		out = []types.SLOWorkspaceItem{}
	}
	return out
}
