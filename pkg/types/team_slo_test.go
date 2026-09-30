package types

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestValidateTeamSLOs(t *testing.T) {
	incident := func(flag bool) *Component {
		return &Component{
			Name: "TRT Incidents", Slug: "trt-incidents", ShipTeam: "Other", SLOComponent: flag,
			Subcomponents: []SubComponent{{Name: "Incidents", Slug: "incidents"}},
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
	withWorkspace := func(slos ...NamedSLO) *DashboardConfig {
		return &DashboardConfig{TeamSLOs: []TeamSLOConfig{{
			Team:   "TRT",
			Owners: []Owner{{User: "chai-bot"}},
			SLOs:   slos,
		}}}
	}
	ws := func() *SLOWorkspace {
		return &SLOWorkspace{Kind: "payload_streams", SchemaVersion: 1}
	}
	known := func(kind string, version int) bool {
		return kind == "payload_streams" && version == 1
	}

	second := team("trt-incidents")
	second.Team = "Widget"

	tests := []struct {
		name    string
		cfg     *DashboardConfig
		known   func(string, int) bool
		wantErr string
	}{
		{
			name: "one workspace",
			cfg: withWorkspace(NamedSLO{
				Name: "accepted-payload-per-day", Source: "payload_acceptance", Workspace: ws(),
			}),
			known: known,
		},
		{
			name: "two workspaces",
			cfg: withWorkspace(
				NamedSLO{Name: "a", Source: "payload_acceptance", Workspace: ws()},
				NamedSLO{Name: "b", Source: "payload_acceptance", Workspace: ws()},
			),
			known:   known,
			wantErr: "only one workspace is allowed",
		},
		{
			name:    "missing owners",
			cfg:     &DashboardConfig{TeamSLOs: []TeamSLOConfig{{Team: "TRT", SLOs: []NamedSLO{{Name: "a", Source: "payload_acceptance"}}}}},
			wantErr: "owners is required",
		},
		{
			name: "unknown schema",
			cfg: withWorkspace(NamedSLO{
				Name: "a", Source: "payload_acceptance",
				Workspace: &SLOWorkspace{Kind: "payload_streams", SchemaVersion: 9},
			}),
			known:   known,
			wantErr: "unknown workspace schema",
		},
		{
			name: "listed flagged component",
			cfg:  &DashboardConfig{Components: []*Component{incident(true)}, TeamSLOs: []TeamSLOConfig{team("trt-incidents")}},
		},
		{
			name:    "missing component",
			cfg:     &DashboardConfig{TeamSLOs: []TeamSLOConfig{team("trt-incidents")}},
			wantErr: `slo_component "trt-incidents" does not exist`,
		},
		{
			name:    "not marked",
			cfg:     &DashboardConfig{Components: []*Component{incident(false)}, TeamSLOs: []TeamSLOConfig{team("trt-incidents")}},
			wantErr: `component "trt-incidents" is not marked slo_component`,
		},
		{
			name:    "flagged but not listed",
			cfg:     &DashboardConfig{Components: []*Component{incident(true)}, TeamSLOs: []TeamSLOConfig{team()}},
			wantErr: `component "trt-incidents" is marked slo_component but is not listed`,
		},
		{
			name:    "duplicate on one team",
			cfg:     &DashboardConfig{Components: []*Component{incident(true)}, TeamSLOs: []TeamSLOConfig{team("trt-incidents", "trt-incidents")}},
			wantErr: `duplicate slo_component "trt-incidents"`,
		},
		{
			name:    "listed on two teams",
			cfg:     &DashboardConfig{Components: []*Component{incident(true)}, TeamSLOs: []TeamSLOConfig{team("trt-incidents"), second}},
			wantErr: `listed on both "TRT" and "Widget"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.ValidateTeamSLOs(tt.known, nil)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestCheckedInDashboardConfigsListSLOComponents(t *testing.T) {
	known := func(kind string, version int) bool {
		return kind == "payload_streams" && version == 1
	}
	for _, path := range []string{
		"../../hack/local/dashboard/config.yaml",
		"../../test/e2e/scripts/dashboard-config.yaml",
	} {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(path)
			require.NoError(t, err)
			var cfg DashboardConfig
			require.NoError(t, yaml.Unmarshal(raw, &cfg))
			cfg.AssignSlugs()
			require.NoError(t, cfg.ValidateTeamSLOs(known, nil))
			require.Len(t, cfg.TeamSLOs, 1)
			assert.Equal(t, []string{"trt-incidents"}, cfg.TeamSLOs[0].SLOComponents)
			require.NotNil(t, cfg.TeamSLOs[0].Workspace())
			assert.NotEmpty(t, cfg.TeamSLOs[0].Workspace().Spec)

			encoded, err := yaml.Marshal(&cfg)
			require.NoError(t, err)
			var again DashboardConfig
			require.NoError(t, yaml.Unmarshal(encoded, &again))
			again.AssignSlugs()
			require.NoError(t, again.ValidateTeamSLOs(known, nil))
			require.NotNil(t, again.TeamSLOs[0].Workspace())
			assert.JSONEq(t, string(cfg.TeamSLOs[0].Workspace().Spec), string(again.TeamSLOs[0].Workspace().Spec))
		})
	}
}

func TestSLOComponentsForTeam(t *testing.T) {
	cfg := &DashboardConfig{
		Components: []*Component{
			{
				Name: "TRT Incidents", Slug: "trt-incidents", ShipTeam: "Other", SLOComponent: true,
				Subcomponents: []SubComponent{{Name: "Incidents", Slug: "incidents"}},
			},
			{
				Name: "Also Flagged", Slug: "also-flagged", ShipTeam: "TRT", SLOComponent: true,
				Subcomponents: []SubComponent{{Name: "Other", Slug: "other"}},
			},
		},
		TeamSLOs: []TeamSLOConfig{{
			Team:          "TRT",
			SLOComponents: []string{"trt-incidents"},
		}},
	}
	outages := map[SubComponentRef][]Outage{
		{ComponentSlug: "trt-incidents", SubSlug: "incidents"}: {{Description: "open"}},
		{ComponentSlug: "also-flagged", SubSlug: "other"}:      {{Description: "ignored"}},
	}

	tests := []struct {
		team      string
		wantComp  string
		wantSub   string
		wantCount int
	}{
		{team: "TRT", wantComp: "TRT Incidents", wantSub: "Incidents", wantCount: 1},
		{team: "Other", wantCount: 0},
	}
	for _, tt := range tests {
		t.Run(tt.team, func(t *testing.T) {
			got := cfg.SLOComponentsForTeam(tt.team, outages)
			require.Len(t, got, tt.wantCount)
			if tt.wantCount == 0 {
				return
			}
			assert.Equal(t, tt.wantComp, got[0].Component)
			assert.Equal(t, tt.wantSub, got[0].SubComponent)
			require.Len(t, got[0].Outages, 1)
			assert.Equal(t, "open", got[0].Outages[0].Description)
		})
	}
}

func TestSummaryTeamNames(t *testing.T) {
	cfg := &DashboardConfig{
		Components: []*Component{{Name: "Incidents", ShipTeam: "Lonely", SLOComponent: true}},
		TeamSLOs:   []TeamSLOConfig{{Team: "TRT"}, {Team: "TRT"}, {Team: ""}},
	}
	assert.Equal(t, []string{"TRT"}, cfg.SummaryTeamNames())
}
