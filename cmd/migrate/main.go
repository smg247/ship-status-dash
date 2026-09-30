package main

import (
	"flag"
	"fmt"

	"github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ship-status-dash/pkg/types"
)

func main() {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	dsn := flag.String("dsn", "postgres://postgres:postgres@localhost:5432/ship_status?sslmode=disable&client_encoding=UTF8", "PostgreSQL DSN connection string")
	flag.Parse()

	if *dsn == "" {
		log.Fatal("DSN cannot be empty")
	}

	log.Info("Connecting to PostgreSQL database")

	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		log.WithField("error", err).Fatal("Failed to connect to database")
	}

	sqlDB, err := db.DB()
	if err != nil {
		log.WithField("error", err).Fatal("Failed to get database instance")
	}

	// Explicitly set client encoding (required for simple protocol queries)
	if _, err := sqlDB.Exec("SET client_encoding = 'UTF8'"); err != nil {
		log.WithField("error", err).Fatal("Failed to set client encoding")
	}

	log.Info("Running migrations...")

	if err = db.AutoMigrate(&types.Outage{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate Outage table")
	}

	if err = db.AutoMigrate(&types.Reason{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate Reason table")
	}

	if err = db.AutoMigrate(&types.ComponentReportPing{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate ComponentReportPing table")
	}

	if err = db.AutoMigrate(&types.SlackThread{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate SlackThread table")
	}

	if err = db.AutoMigrate(&types.OutageAuditLog{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate OutageAuditLog table")
	}

	// TODO(sgoeddel): remove once all environments have run this backfill
	// Backfill last_auditable_update from the newest audit log (or created_at when none exist).
	if err = db.Exec(`
		UPDATE outages o
		SET last_auditable_update = COALESCE(
			(SELECT MAX(a.created_at) FROM outage_audit_logs a WHERE a.outage_id = o.id AND a.deleted_at IS NULL),
			o.created_at
		)
		WHERE o.last_auditable_update IS NULL
		   OR o.last_auditable_update = TIMESTAMPTZ '0001-01-01 00:00:00+00'
	`).Error; err != nil {
		log.WithField("error", err).Fatal("Failed to backfill last_auditable_update")
	}

	// Keep last_auditable_update in sync with audit log inserts (source of truth for change detection).
	if err = db.Exec(`
		CREATE OR REPLACE FUNCTION sync_outage_last_auditable_update()
		RETURNS TRIGGER AS $$
		BEGIN
			UPDATE outages
			SET last_auditable_update = NEW.created_at
			WHERE id = NEW.outage_id;
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql
	`).Error; err != nil {
		log.WithField("error", err).Fatal("Failed to create sync_outage_last_auditable_update function")
	}
	if err = db.Exec(`
		DROP TRIGGER IF EXISTS trg_sync_outage_last_auditable_update ON outage_audit_logs
	`).Error; err != nil {
		log.WithField("error", err).Fatal("Failed to drop trg_sync_outage_last_auditable_update")
	}
	if err = db.Exec(`
		CREATE TRIGGER trg_sync_outage_last_auditable_update
		AFTER INSERT ON outage_audit_logs
		FOR EACH ROW
		EXECUTE PROCEDURE sync_outage_last_auditable_update()
	`).Error; err != nil {
		log.WithField("error", err).Fatal("Failed to create trg_sync_outage_last_auditable_update")
	}

	if err = db.AutoMigrate(&types.TriageNote{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate TriageNote table")
	}

	if err = db.AutoMigrate(&types.OutageLink{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate OutageLink table")
	}

	if err = db.AutoMigrate(&types.OutageReport{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate OutageReport table")
	}

	if err = db.AutoMigrate(&types.OutageRelationship{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate OutageRelationship table")
	}

	if err = db.AutoMigrate(&types.SLOWorkspaceItem{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate SLOWorkspaceItem table")
	}

	if err = db.AutoMigrate(&types.SLOWorkspaceLink{}); err != nil {
		log.WithField("error", err).Fatal("Failed to migrate SLOWorkspaceLink table")
	}

	db.Exec("DROP INDEX IF EXISTS idx_one_active_suspected_per_subcomponent")
	if err = db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_one_active_suspected_per_subcomponent
		ON outages (component_name, sub_component_name)
		WHERE end_time IS NULL AND severity = 'Suspected' AND deleted_at IS NULL`).Error; err != nil {
		log.WithField("error", err).Fatal("Failed to create unique partial index for suspected outages")
	}

	log.Info("Migration completed successfully")

	var tableCount int64
	db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE'").Scan(&tableCount)

	log.Infof("Database contains %d tables", tableCount)

	if err := sqlDB.Close(); err != nil {
		log.WithField("error", err).Warn("Failed to close database")
	}

	fmt.Println("\n✓ Migration complete")
}
