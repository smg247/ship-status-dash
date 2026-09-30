package slo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	payloadv1 "ship-status-dash/pkg/slo/payloadstreams/v1"
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
