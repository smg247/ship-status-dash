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

type TRTSLOPayloadPruner struct {
	configManager *config.Manager[types.DashboardConfig]
	sloRepo       repositories.SLOWorkspaceRepository
	checkInterval time.Duration
	logger        *logrus.Logger
}

func NewTRTSLOPayloadPruner(configManager *config.Manager[types.DashboardConfig], sloRepo repositories.SLOWorkspaceRepository, checkInterval time.Duration, logger *logrus.Logger) *TRTSLOPayloadPruner {
	return &TRTSLOPayloadPruner{
		configManager: configManager,
		sloRepo:       sloRepo,
		checkInterval: checkInterval,
		logger:        logger,
	}
}

func (p *TRTSLOPayloadPruner) Start(ctx context.Context) {
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

func (p *TRTSLOPayloadPruner) prune() {
	logger := p.logger.WithField("check", "trt_slo_payload_prune")
	logger.Info("Pruning expired TRT payload items")

	cfg := p.configManager.Get()
	if cfg == nil {
		return
	}
	now := time.Now().UTC()
	for i := range cfg.TeamSLOs {
		sloCfg := &cfg.TeamSLOs[i]
		ws := sloCfg.Workspace()
		if ws == nil || !slo.KnownWorkspace(ws.Kind, ws.SchemaVersion) {
			continue
		}
		teamLogger := logger.WithField("team", sloCfg.Team)
		err := p.sloRepo.PruneTeamItems(sloCfg.Team, func(items []types.SLOWorkspaceItem) []uint {
			ids, err := slo.PruneIDs(now, ws, items)
			if err != nil {
				teamLogger.WithField("error", err).Error("Failed to select TRT payload items")
				return nil
			}
			return ids
		})
		if err != nil {
			teamLogger.WithField("error", err).Error("Failed to prune TRT payload items")
		}
	}
}
