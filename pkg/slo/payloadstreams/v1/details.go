package v1

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// PayloadDetails is the payload_streams schema version 1 document.
type PayloadDetails struct {
	PayloadURL  string       `json:"payload_url"`
	AnalysisURL string       `json:"analysis_url,omitempty"`
	Jobs        []PayloadJob `json:"jobs"`
}

// PayloadJob is one blocking job on a payload_streams v1 row.
type PayloadJob struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	State          string `json:"state"`
	Notes          string `json:"notes,omitempty"`
	RecurringCount *int   `json:"recurring_count,omitempty"`
}

// DecodeDetails parses a payload_streams v1 document.
func DecodeDetails(details []byte) (PayloadDetails, error) {
	if len(bytes.TrimSpace(details)) == 0 {
		return PayloadDetails{}, fmt.Errorf("details is required")
	}
	dec := json.NewDecoder(bytes.NewReader(details))
	dec.DisallowUnknownFields()
	var doc PayloadDetails
	if err := dec.Decode(&doc); err != nil {
		return PayloadDetails{}, fmt.Errorf("invalid payload_streams v1 details: %w", err)
	}
	if dec.More() {
		return PayloadDetails{}, fmt.Errorf("invalid payload_streams v1 details: trailing data")
	}
	return doc, nil
}

// Validate checks a decoded payload_streams v1 document.
func (d PayloadDetails) Validate() error {
	if strings.TrimSpace(d.PayloadURL) == "" {
		return fmt.Errorf("payload_url is required")
	}
	if d.Jobs == nil {
		return fmt.Errorf("jobs is required")
	}
	for i, job := range d.Jobs {
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

// ValidateDetails decodes details and checks the payload_streams v1 document.
func ValidateDetails(details []byte) error {
	doc, err := DecodeDetails(details)
	if err != nil {
		return err
	}
	return doc.Validate()
}
