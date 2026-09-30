package slo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
	"ship-status-dash/pkg/types"
)

func TestValidateDetails(t *testing.T) {
	ok := []byte(`{
		"payload_url": "https://example.test/release",
		"analysis_url": "https://example.test/analysis.html",
		"jobs": [{"name": "e2e", "url": "https://prow.example/job", "state": "failure", "notes": "flake", "recurring_count": 3}]
	}`)
	tests := []struct {
		name    string
		kind    string
		version int
		details []byte
		wantErr string
	}{
		{name: "v1", kind: payloadv1.Kind, version: payloadv1.SchemaVersion, details: ok},
		{name: "unknown field", kind: payloadv1.Kind, version: 1, details: []byte(`{"payload_url":"https://example.test","jobs":[],"extra":1}`), wantErr: "invalid payload_streams"},
		{name: "unknown version", kind: payloadv1.Kind, version: 2, details: ok, wantErr: "unknown workspace schema"},
		{name: "missing payload url", kind: payloadv1.Kind, version: 1, details: []byte(`{"jobs":[]}`), wantErr: "payload_url is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDetails(tt.kind, tt.version, tt.details)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestValidateSettings(t *testing.T) {
	valid := func(mutate func(*payloadv1.Settings)) *types.SLOWorkspace {
		settings := payloadv1.Settings{
			Window: "48h", MinAccepted: 3, RecentPayloads: 4,
			Streams: []payloadv1.Stream{{ReleaseController: "amd64", Name: "5.1.0-0.nightly"}},
		}
		if mutate != nil {
			mutate(&settings)
		}
		raw, err := json.Marshal(settings)
		require.NoError(t, err)
		return &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: payloadv1.SchemaVersion, Spec: raw}
	}

	tests := []struct {
		name    string
		ws      *types.SLOWorkspace
		wantErr string
	}{
		{name: "non-default configuration", ws: valid(nil)},
		{name: "empty spec", ws: &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: 1}, wantErr: "workspace settings are required"},
		{
			name:    "unknown field",
			ws:      &types.SLOWorkspace{Kind: payloadv1.Kind, SchemaVersion: 1, Spec: []byte(`{"window":"24h","min_accepted":1,"recent_payloads":1,"streams":[{"release_controller":"amd64","name":"n"}],"extra":true}`)},
			wantErr: "invalid payload_streams",
		},
		{name: "invalid window", ws: valid(func(s *payloadv1.Settings) { s.Window = "nope" }), wantErr: "window must be a positive duration"},
		{name: "nonpositive window", ws: valid(func(s *payloadv1.Settings) { s.Window = "0s" }), wantErr: "window must be a positive duration"},
		{name: "negative window", ws: valid(func(s *payloadv1.Settings) { s.Window = "-1h" }), wantErr: "window must be a positive duration"},
		{name: "nonpositive min_accepted", ws: valid(func(s *payloadv1.Settings) { s.MinAccepted = 0 }), wantErr: "min_accepted must be positive"},
		{name: "nonpositive recent_payloads", ws: valid(func(s *payloadv1.Settings) { s.RecentPayloads = 0 }), wantErr: "recent_payloads must be positive"},
		{name: "missing streams", ws: valid(func(s *payloadv1.Settings) { s.Streams = nil }), wantErr: "streams is required"},
		{name: "blank stream name", ws: valid(func(s *payloadv1.Settings) {
			s.Streams = []payloadv1.Stream{{ReleaseController: "amd64", Name: " "}}
		}), wantErr: "stream name is required"},
		{name: "duplicate stream", ws: valid(func(s *payloadv1.Settings) {
			s.Streams = []payloadv1.Stream{
				{ReleaseController: "amd64", Name: "nightly"},
				{ReleaseController: "arm64", Name: "nightly"},
			}
		}), wantErr: `duplicate stream "nightly"`},
		{name: "missing release controller", ws: valid(func(s *payloadv1.Settings) {
			s.Streams = []payloadv1.Stream{{Name: "nightly"}}
		}), wantErr: "release_controller is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSettings(tt.ws)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
