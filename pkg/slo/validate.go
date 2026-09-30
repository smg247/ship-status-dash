package slo

import (
	"fmt"
	"time"

	"ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

// KnownWorkspace reports whether this build can validate and render the pair.
func KnownWorkspace(kind string, schemaVersion int) bool {
	return kind == v1.Kind && schemaVersion == v1.SchemaVersion
}

// ValidateSettings checks schema-specific workspace settings.
func ValidateSettings(ws *types.SLOWorkspace) error {
	if ws == nil || !KnownWorkspace(ws.Kind, ws.SchemaVersion) {
		return fmt.Errorf("unknown workspace schema")
	}
	_, err := v1.ParseSettings(ws.Spec)
	return err
}

// ValidateDetails checks details against the schema for kind and schemaVersion.
func ValidateDetails(kind string, schemaVersion int, details []byte) error {
	if !KnownWorkspace(kind, schemaVersion) {
		return fmt.Errorf("unknown workspace schema %s v%d", kind, schemaVersion)
	}
	return v1.ValidateDetails(details)
}

// PruneIDs returns workspace rows the registered evaluator wants deleted.
func PruneIDs(now time.Time, ws *types.SLOWorkspace, items []types.SLOWorkspaceItem) ([]uint, error) {
	if ws == nil || !KnownWorkspace(ws.Kind, ws.SchemaVersion) {
		return nil, nil
	}
	settings, err := v1.ParseSettings(ws.Spec)
	if err != nil {
		return nil, err
	}
	return v1.PruneIDs(now, settings, items), nil
}

// ValidLinkType reports whether linkType is jira, outage, or other.
func ValidLinkType(linkType string) bool {
	switch linkType {
	case "jira", "outage", "other":
		return true
	default:
		return false
	}
}
