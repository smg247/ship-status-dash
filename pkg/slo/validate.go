package slo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	KindPayloadStreams = "payload_streams"
	SchemaVersionV1    = 1
)

// PayloadDetailsV1 is the payload_streams schema version 1 document.
type PayloadDetailsV1 struct {
	PayloadURL  string         `json:"payload_url"`
	AnalysisURL string         `json:"analysis_url,omitempty"`
	Jobs        []PayloadJobV1 `json:"jobs"`
}

// PayloadJobV1 is one blocking job on a payload_streams v1 row.
type PayloadJobV1 struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	State          string `json:"state"`
	Notes          string `json:"notes,omitempty"`
	RecurringCount *int   `json:"recurring_count,omitempty"`
}

// KnownWorkspace reports whether this build can validate and render the pair.
func KnownWorkspace(kind string, schemaVersion int) bool {
	return kind == KindPayloadStreams && schemaVersion == SchemaVersionV1
}

// ValidateDetails checks details against the schema for kind and schemaVersion.
func ValidateDetails(kind string, schemaVersion int, details []byte) error {
	if !KnownWorkspace(kind, schemaVersion) {
		return fmt.Errorf("unknown workspace schema %s v%d", kind, schemaVersion)
	}
	if len(bytes.TrimSpace(details)) == 0 {
		return fmt.Errorf("details is required")
	}
	dec := json.NewDecoder(bytes.NewReader(details))
	dec.DisallowUnknownFields()
	var doc PayloadDetailsV1
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("invalid payload_streams v1 details: %w", err)
	}
	if dec.More() {
		return fmt.Errorf("invalid payload_streams v1 details: trailing data")
	}
	if strings.TrimSpace(doc.PayloadURL) == "" {
		return fmt.Errorf("payload_url is required")
	}
	if doc.Jobs == nil {
		return fmt.Errorf("jobs is required")
	}
	for i, job := range doc.Jobs {
		if strings.TrimSpace(job.Name) == "" {
			return fmt.Errorf("jobs[%d].name is required", i)
		}
		if strings.TrimSpace(job.URL) == "" {
			return fmt.Errorf("jobs[%d].url is required", i)
		}
		if strings.TrimSpace(job.State) == "" {
			return fmt.Errorf("jobs[%d].state is required", i)
		}
		if job.RecurringCount != nil && *job.RecurringCount < 0 {
			return fmt.Errorf("jobs[%d].recurring_count must be >= 0", i)
		}
	}
	return nil
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
