package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// Kind is the payload_streams workspace kind.
	Kind = "payload_streams"
	// SchemaVersion is the published payload_streams document version.
	SchemaVersion = 1
	// Source is the evaluator registered for this workspace.
	Source = "payload_acceptance"
)

// Settings is the payload_streams v1 workspace configuration.
type Settings struct {
	Window         string   `json:"window"`
	MinAccepted    int      `json:"min_accepted"`
	RecentPayloads int      `json:"recent_payloads"`
	Streams        []Stream `json:"streams"`
}

// Stream is one watched release stream.
type Stream struct {
	ReleaseController string `json:"release_controller"`
	Name              string `json:"name"`
}

// ParseSettings decodes and checks schema-specific workspace settings.
func ParseSettings(spec json.RawMessage) (Settings, error) {
	if len(bytes.TrimSpace(spec)) == 0 {
		return Settings{}, fmt.Errorf("workspace settings are required")
	}
	dec := json.NewDecoder(bytes.NewReader(spec))
	dec.DisallowUnknownFields()
	var settings Settings
	if err := dec.Decode(&settings); err != nil {
		return Settings{}, fmt.Errorf("invalid payload_streams v1 settings: %w", err)
	}
	if err := settings.Validate(); err != nil {
		return Settings{}, err
	}
	return settings, nil
}

// Validate checks the payload_streams v1 settings.
func (s Settings) Validate() error {
	window, err := time.ParseDuration(s.Window)
	if err != nil || window <= 0 {
		return fmt.Errorf("window must be a positive duration")
	}
	if s.MinAccepted <= 0 {
		return fmt.Errorf("min_accepted must be positive")
	}
	if s.RecentPayloads <= 0 {
		return fmt.Errorf("recent_payloads must be positive")
	}
	if len(s.Streams) == 0 {
		return fmt.Errorf("streams is required")
	}
	seen := sets.New[string]()
	for _, stream := range s.Streams {
		name := strings.TrimSpace(stream.Name)
		if name == "" {
			return fmt.Errorf("stream name is required")
		}
		if seen.Has(name) {
			return fmt.Errorf("duplicate stream %q", name)
		}
		seen.Insert(name)
		if strings.TrimSpace(stream.ReleaseController) == "" {
			return fmt.Errorf("stream %q: release_controller is required", name)
		}
	}
	return nil
}

// WindowDuration returns the configured acceptance window.
func (s Settings) WindowDuration() time.Duration {
	window, err := time.ParseDuration(s.Window)
	if err != nil {
		return 0
	}
	return window
}
