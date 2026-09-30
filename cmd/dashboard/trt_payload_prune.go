package main

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
)

// TRTPayloadPruner deletes TRT payload_streams rows that have fallen outside retention.
// Reads stay side-effect free; this loop is the only persistent cleanup for that workspace.
type TRTPayloadPruner struct {
	configManager *config.Manager[types.DashboardConfig]
	sloRepo       repositories.SLOWorkspaceRepository
	checkInterval time.Duration
	logger        *logrus.Logger
}

// NewTRTPayloadPruner creates a pruner that runs on checkInterval.
func NewTRTPayloadPruner(configManager *config.Manager[types.DashboardConfig], sloRepo repositories.SLOWorkspaceRepository, checkInterval time.Duration, logger *logrus.Logger) *TRTPayloadPruner {
	return &TRTPayloadPruner{
		configManager: configManager,
		sloRepo:       sloRepo,
		checkInterval: checkInterval,
		logger:        logger,
	}
}

// Start prunes once, then on each tick, until ctx is cancelled.
func (p *TRTPayloadPruner) Start(ctx context.Context) {
	p.logger.WithField("check_interval", p.checkInterval).Info("Starting TRT payload pruner")
	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	p.prune()
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("Stopping TRT payload pruner")
			return
		case <-ticker.C:
			p.prune()
		}
	}
}

func (p *TRTPayloadPruner) prune() {
	logger := p.logger.WithField("check", "trt_payload_prune")
	logger.Info("Pruning expired TRT payload items")

	cfg := p.configManager.Get()
	if cfg == nil {
		return
	}
	now := time.Now().UTC()
	for i := range cfg.TeamSLOs {
		teamCfg := &cfg.TeamSLOs[i]
		ws := teamCfg.Workspace()
		if !slo.IsTRTPayloadWorkspace(ws) {
			continue
		}
		teamLogger := logger.WithField("team", teamCfg.Team)
		names := streamNames(ws)
		recent := ws.RecentPayloads
		err := p.sloRepo.PruneTeamItems(teamCfg.Team, func(items []types.SLOWorkspaceItem) []uint {
			return slo.TRTPayloadPruneIDs(now, names, recent, items)
		})
		if err != nil {
			teamLogger.WithField("error", err).Error("Failed to prune TRT payload items")
		}
	}
}
