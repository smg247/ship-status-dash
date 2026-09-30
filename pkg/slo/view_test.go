package slo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ship-status-dash/pkg/types"
)

func TestSLOComponentsForTeamUsesExplicitList(t *testing.T) {
	cfg := &types.DashboardConfig{
		Components: []*types.Component{
			{
				Name: "TRT Incidents", Slug: "trt-incidents", ShipTeam: "Other", SLOComponent: true,
				Subcomponents: []types.SubComponent{{Name: "Incidents", Slug: "incidents"}},
			},
			{
				Name: "Also Flagged", Slug: "also-flagged", ShipTeam: "TRT", SLOComponent: true,
				Subcomponents: []types.SubComponent{{Name: "Other", Slug: "other"}},
			},
		},
		TeamSLOs: []types.TeamSLOConfig{{
			Team:          "TRT",
			SLOComponents: []string{"trt-incidents"},
		}},
	}
	outages := map[types.SubComponentRef][]types.Outage{
		{ComponentSlug: "trt-incidents", SubSlug: "incidents"}: {{Description: "open"}},
		{ComponentSlug: "also-flagged", SubSlug: "other"}:      {{Description: "ignored"}},
	}

	got := SLOComponentsForTeam(cfg, "TRT", outages)
	require.Len(t, got, 1)
	assert.Equal(t, "TRT Incidents", got[0].Component)
	assert.Equal(t, "Incidents", got[0].SubComponent)
	require.Len(t, got[0].Outages, 1)
	assert.Equal(t, "open", got[0].Outages[0].Description)
	assert.Empty(t, SLOComponentsForTeam(cfg, "Other", outages))
}

func TestSummaryTeamNamesIgnoresUnlistedSLOComponent(t *testing.T) {
	cfg := &types.DashboardConfig{
		Components: []*types.Component{
			{Name: "Incidents", ShipTeam: "Lonely", SLOComponent: true},
		},
		TeamSLOs: []types.TeamSLOConfig{{Team: "TRT"}},
	}
	assert.Equal(t, []string{"TRT"}, SummaryTeamNames(cfg))
}
