package types

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValidateTeamSLOs(t *testing.T) {
	valid := &DashboardConfig{
		TeamSLOs: []TeamSLOConfig{{
			Team:   "TRT",
			Owners: []Owner{{User: "chai-bot"}},
			SLOs: []NamedSLO{{
				Name:   "accepted-payload-per-day",
				Source: "payload_acceptance",
				Workspace: &SLOWorkspace{
					Kind:          "payload_streams",
					SchemaVersion: 1,
					Streams:       []SLOStream{{Name: "5.1.0-0.nightly"}},
				},
			}},
		}},
	}
	require.NoError(t, valid.ValidateTeamSLOs())

	two := &DashboardConfig{
		TeamSLOs: []TeamSLOConfig{{
			Team:   "TRT",
			Owners: []Owner{{User: "chai-bot"}},
			SLOs: []NamedSLO{
				{Name: "a", Source: "payload_acceptance", Workspace: &SLOWorkspace{Kind: "payload_streams", SchemaVersion: 1}},
				{Name: "b", Source: "payload_acceptance", Workspace: &SLOWorkspace{Kind: "payload_streams", SchemaVersion: 1}},
			},
		}},
	}
	assert.Error(t, two.ValidateTeamSLOs())

	missingOwners := &DashboardConfig{
		TeamSLOs: []TeamSLOConfig{{Team: "TRT", SLOs: []NamedSLO{{Name: "a", Source: "payload_acceptance"}}}},
	}
	assert.Error(t, missingOwners.ValidateTeamSLOs())
}

func TestValidateTeamSLOComponents(t *testing.T) {
	incident := func(flag bool) *Component {
		return &Component{
			Name: "TRT Incidents", ShipTeam: "Other", SLOComponent: flag,
			Subcomponents: []SubComponent{{Name: "Incidents"}},
		}
	}
	team := func(slugs ...string) TeamSLOConfig {
		return TeamSLOConfig{
			Team:          "TRT",
			Owners:        []Owner{{User: "chai-bot"}},
			SLOComponents: slugs,
			SLOs:          []NamedSLO{{Name: "accepted-payload-per-day", Source: "payload_acceptance"}},
		}
	}

	t.Run("listed flagged component", func(t *testing.T) {
		cfg := &DashboardConfig{
			Components: []*Component{incident(true)},
			TeamSLOs:   []TeamSLOConfig{team("trt-incidents")},
		}
		require.NoError(t, cfg.ValidateTeamSLOs())
	})

	t.Run("missing component", func(t *testing.T) {
		cfg := &DashboardConfig{TeamSLOs: []TeamSLOConfig{team("trt-incidents")}}
		err := cfg.ValidateTeamSLOs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `slo_component "trt-incidents" does not exist`)
	})

	t.Run("not marked", func(t *testing.T) {
		cfg := &DashboardConfig{
			Components: []*Component{incident(false)},
			TeamSLOs:   []TeamSLOConfig{team("trt-incidents")},
		}
		err := cfg.ValidateTeamSLOs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `component "trt-incidents" is not marked slo_component`)
	})

	t.Run("flagged but not listed", func(t *testing.T) {
		cfg := &DashboardConfig{
			Components: []*Component{incident(true)},
			TeamSLOs:   []TeamSLOConfig{team()},
		}
		err := cfg.ValidateTeamSLOs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `component "trt-incidents" is marked slo_component but is not listed`)
	})

	t.Run("duplicate on one team", func(t *testing.T) {
		cfg := &DashboardConfig{
			Components: []*Component{incident(true)},
			TeamSLOs:   []TeamSLOConfig{team("trt-incidents", "trt-incidents")},
		}
		err := cfg.ValidateTeamSLOs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `duplicate slo_component "trt-incidents"`)
	})

	t.Run("listed on two teams", func(t *testing.T) {
		second := team("trt-incidents")
		second.Team = "Widget"
		cfg := &DashboardConfig{
			Components: []*Component{incident(true)},
			TeamSLOs:   []TeamSLOConfig{team("trt-incidents"), second},
		}
		err := cfg.ValidateTeamSLOs()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `listed on both "TRT" and "Widget"`)
	})
}

func TestCheckedInDashboardConfigsListSLOComponents(t *testing.T) {
	for _, path := range []string{
		"../../hack/local/dashboard/config.yaml",
		"../../test/e2e/scripts/dashboard-config.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var cfg DashboardConfig
			require.NoError(t, yaml.Unmarshal(raw, &cfg))
			require.NoError(t, cfg.ValidateTeamSLOs())
			require.Len(t, cfg.TeamSLOs, 1)
			assert.Equal(t, []string{"trt-incidents"}, cfg.TeamSLOs[0].SLOComponents)
		})
	}
}

func TestNormalizeTeamSLOsDefaultsRecent(t *testing.T) {
	cfg := &DashboardConfig{
		TeamSLOs: []TeamSLOConfig{{
			SLOs: []NamedSLO{{Workspace: &SLOWorkspace{Kind: "payload_streams", SchemaVersion: 1}}},
		}},
	}
	cfg.NormalizeTeamSLOs()
	assert.Equal(t, 5, cfg.TeamSLOs[0].SLOs[0].Workspace.RecentPayloads)
}
