package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/slo/seed"
	"ship-status-dash/pkg/types"
)

func main() {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	dsn := flag.String("dsn", "", "PostgreSQL DSN connection string")
	configPath := flag.String("config", "hack/local/dashboard/config.yaml", "Dashboard config used for team and stream names")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("DSN cannot be empty")
	}

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.WithField("error", err).Fatal("Failed to load config")
	}

	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		log.WithField("error", err).Fatal("Failed to connect to database")
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.WithField("error", err).Fatal("Failed to get database instance")
	}
	defer sqlDB.Close()
	if _, err := sqlDB.Exec("SET client_encoding = 'UTF8'"); err != nil {
		log.WithField("error", err).Fatal("Failed to set client encoding")
	}

	repo := repositories.NewGORMSLOWorkspaceRepository(db)
	if err := seed.Apply(db, repo, cfg); err != nil {
		log.WithField("error", err).Fatal("Failed to seed SLO workspace")
	}
	fmt.Printf("\n✓ Seeded SLO workspace rows\n")
}

func loadConfig(path string) (*types.DashboardConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg types.DashboardConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	cfg.AssignSlugs()
	if err := cfg.ValidateTeamSLOs(slo.KnownWorkspace, slo.ValidateSettings); err != nil {
		return nil, err
	}
	return &cfg, nil
}
