package slo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDetailsV1(t *testing.T) {
	ok := []byte(`{
		"payload_url": "https://example.test/release",
		"analysis_url": "https://example.test/analysis.html",
		"jobs": [{"name": "e2e", "url": "https://prow.example/job", "state": "failure", "notes": "flake", "recurring_count": 3}]
	}`)
	require.NoError(t, ValidateDetails(KindPayloadStreams, 1, ok))

	err := ValidateDetails(KindPayloadStreams, 1, []byte(`{"payload_url":"https://example.test","jobs":[],"extra":1}`))
	assert.Error(t, err)

	err = ValidateDetails(KindPayloadStreams, 2, ok)
	assert.Error(t, err)

	err = ValidateDetails(KindPayloadStreams, 1, []byte(`{"jobs":[]}`))
	assert.Error(t, err)
}
