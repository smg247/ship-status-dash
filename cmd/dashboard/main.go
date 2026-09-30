package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	apimachineryerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"

	"ship-status-dash/pkg/auth"
	"ship-status-dash/pkg/config"
	"ship-status-dash/pkg/outage"
	"ship-status-dash/pkg/repositories"
	"ship-status-dash/pkg/slo"
	"ship-status-dash/pkg/types"
	"ship-status-dash/pkg/utils"
)

// Options contains command-line configuration options for the dashboard server.
type Options struct {
	ConfigPath                string
	Port                      string
	DatabaseDSN               string
	HMACSecretFile            string
	CORSOrigin                string
	KubeconfigPath            string
	AbsentReportCheckInterval time.Duration
	TRTPayloadPruneInterval   time.Duration
	ConfigUpdatePollInterval  time.Duration
	SlackBaseURL              string
	SlackWorkspaceURL         string
}

// NewOptions parses command-line flags and returns a new Options instance.
func NewOptions() *Options {
	opts := &Options{}

	flag.StringVar(&opts.ConfigPath, "config", "", "Path to config file")
	flag.StringVar(&opts.Port, "port", "8080", "Port to listen on")
	flag.StringVar(&opts.DatabaseDSN, "dsn", "", "PostgreSQL DSN connection string")
	flag.StringVar(&opts.HMACSecretFile, "hmac-secret-file", "", "File containing HMAC secret")
	flag.StringVar(&opts.CORSOrigin, "cors-origin", "*", "CORS allowed origin")
	flag.StringVar(&opts.KubeconfigPath, "kubeconfig", "", "Path to kubeconfig file (empty string uses in-cluster config)")
	flag.DurationVar(&opts.AbsentReportCheckInterval, "absent-report-check-interval", 5*time.Minute, "Interval for checking absent monitored component reports")
	flag.DurationVar(&opts.TRTPayloadPruneInterval, "trt-payload-prune-interval", 30*time.Minute, "Interval for deleting TRT payload items outside retention")
	flag.DurationVar(&opts.ConfigUpdatePollInterval, "config-update-poll-interval", config.DefaultPollInterval, "Interval for polling config file for changes")
	flag.StringVar(&opts.SlackBaseURL, "slack-base-url", "", "Base URL for building outage links in Slack messages. Required if slack reporting is enabled.")
	flag.StringVar(&opts.SlackWorkspaceURL, "slack-workspace-url", "https://rhsandbox.slack.com/", "Slack workspace URL for constructing thread links. Required if slack reporting is enabled.")
	flag.Parse()

	return opts
}

// Validate checks that all required options are provided and valid.
func (o *Options) Validate() error {
	var errs []error

	if o.ConfigPath == "" {
		errs = append(errs, errors.New("config path is required (use --config flag)"))
	} else if _, err := os.Stat(o.ConfigPath); os.IsNotExist(err) {
		errs = append(errs, errors.New("config file does not exist: "+o.ConfigPath))
	}

	if o.Port == "" {
		errs = append(errs, errors.New("port cannot be empty"))
	}

	if o.DatabaseDSN == "" {
		errs = append(errs, errors.New("database DSN is required (use --dsn flag)"))
	}

	if os.Getenv("SKIP_AUTH") != "1" {
		if o.HMACSecretFile == "" {
			errs = append(errs, errors.New("hmac secret file is required (use --hmac-secret-file flag)"))
		} else if _, err := os.Stat(o.HMACSecretFile); os.IsNotExist(err) {
			errs = append(errs, errors.New("hmac secret file does not exist: "+o.HMACSecretFile))
		}
	}

	if os.Getenv("SLACK_BOT_TOKEN") != "" {
		if o.SlackBaseURL == "" {
			errs = append(errs, errors.New("slack-base-url is required when SLACK_BOT_TOKEN is set (use --slack-base-url flag)"))
		}
		if o.SlackWorkspaceURL == "" {
			errs = append(errs, errors.New("slack-workspace-url is required when SLACK_BOT_TOKEN is set (use --slack-workspace-url flag)"))
		}
	}

	return apimachineryerrors.NewAggregate(errs)
}

func setupLogger() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.InfoLevel)
	log.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	return log
}

func loadAndValidateConfig(log *logrus.Logger, configPath string) (*types.DashboardConfig, error) {
	log.Infof("Loading config from %s", configPath)

	configFile, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg types.DashboardConfig
	if err := yaml.Unmarshal(configFile, &cfg); err != nil {
		return nil, err
	}

	for _, component := range cfg.Components {
		if len(component.Owners) == 0 {
			return nil, fmt.Errorf("component must have at least one owner: %s", component.Name)
		}
	}

	cfg.AssignSlugs()
	for _, component := range cfg.Components {
		for i := range component.Subcomponents {
			if component.Subcomponents[i].ReportThreshold <= 0 {
				component.Subcomponents[i].ReportThreshold = types.DefaultReportThreshold
			}
		}
	}

	if err := cfg.ValidateTeamSLOs(slo.KnownWorkspace, slo.ValidateSettings); err != nil {
		return nil, err
	}

	// Validate tags: check that all used tags exist in cfg.Tags
	for _, component := range cfg.Components {
		for _, sub := range component.Subcomponents {
			for _, tagName := range sub.Tags {
				found := false
				for _, configTag := range cfg.Tags {
					if utils.Slugify(configTag.Name) == utils.Slugify(tagName) {
						found = true
						break
					}
				}
				if !found {
					if len(cfg.Tags) == 0 {
						return nil, fmt.Errorf("tag %q used on sub-component %s/%s but no tags are defined in config", tagName, component.Name, sub.Name)
					}
					return nil, fmt.Errorf("tag %q used on sub-component %s/%s but not defined in config tags", tagName, component.Name, sub.Name)
				}
			}
		}
	}

	log.Infof("Loaded configuration with %d components", len(cfg.Components))
	return &cfg, nil
}

func connectDatabase(log *logrus.Logger, dsn string) *gorm.DB {
	log.Info("Connecting to PostgreSQL database")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		// Required for errors.Is checks against gorm.ErrDuplicatedKey to work
		TranslateError: true,
	})
	if err != nil {
		log.WithField("error", err).Fatal("Failed to connect to database")
	}
	return db
}

func getHMACSecret(log *logrus.Logger, path string) []byte {
	if os.Getenv("SKIP_AUTH") == "1" {
		return []byte{}
	}
	secret, err := os.ReadFile(path)
	if err != nil {
		log.WithField("error", err).Warn("Failed to read HMAC secret file")
		return []byte{}
	}
	return []byte(strings.TrimSpace(string(secret)))
}

func extractRoverGroups(config *types.DashboardConfig) []string {
	groupSet := sets.NewString()
	for _, component := range config.Components {
		for _, owner := range component.Owners {
			if owner.RoverGroup != "" {
				groupSet.Insert(owner.RoverGroup)
			}
		}
	}
	for _, slo := range config.TeamSLOs {
		for _, owner := range slo.Owners {
			if owner.RoverGroup != "" {
				groupSet.Insert(owner.RoverGroup)
			}
		}
	}

	return groupSet.List()
}

func loadGroupMembership(log *logrus.Logger, config *types.DashboardConfig, kubeconfigPath string) *auth.GroupMembershipCache {
	groupNames := extractRoverGroups(config)
	if len(groupNames) == 0 {
		log.Info("No rover_groups configured, skipping group membership loading")
		return auth.NewGroupMembershipCache(log)
	}

	log.WithField("group_count", len(groupNames)).Info("Loading group membership from OpenShift")
	cache := auth.NewGroupMembershipCache(log)
	if err := cache.LoadGroups(groupNames, kubeconfigPath); err != nil {
		log.WithField("error", err).Fatal("Failed to load group membership")
	}

	log.Info("Successfully loaded group membership")
	return cache
}

func main() {
	log := setupLogger()
	opts := NewOptions()

	if err := opts.Validate(); err != nil {
		log.WithField("error", err).Fatal("Invalid command-line options")
	}

	loadFunc := func(path string) (*types.DashboardConfig, error) {
		return loadAndValidateConfig(log, path)
	}

	configManager, err := config.NewManager(opts.ConfigPath, loadFunc, log, opts.ConfigUpdatePollInterval)
	if err != nil {
		log.WithField("error", err).Fatal("Failed to create config manager")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := configManager.Watch(ctx); err != nil {
		log.WithField("error", err).Fatal("Failed to start config watcher")
	}

	db := connectDatabase(log, opts.DatabaseDSN)
	hmacSecret := getHMACSecret(log, opts.HMACSecretFile)
	groupCache := loadGroupMembership(log, configManager.Get(), opts.KubeconfigPath)

	configManager.OnUpdate(func(newConfig *types.DashboardConfig) {
		log.Info("Config updated, reloading group membership")
		newGroups := extractRoverGroups(newConfig)
		if err := groupCache.LoadGroups(newGroups, opts.KubeconfigPath); err != nil {
			log.WithField("error", err).Error("Failed to reload group membership")
		} else {
			log.Info("Successfully reloaded group membership")
		}
	})

	var slackClient *slack.Client
	slackToken := os.Getenv("SLACK_BOT_TOKEN")
	if slackToken != "" {
		slackClient = slack.New(slackToken)
		log.Info("Slack integration enabled")
	} else {
		log.Info("Slack integration disabled (SLACK_BOT_TOKEN not set)")
	}

	outageManager := outage.NewDBOutageManager(
		db,
		slackClient,
		configManager,
		opts.SlackBaseURL,
		opts.SlackWorkspaceURL,
		log,
	)

	if err := outageManager.ResolveActiveOutagesForMissingSubComponents(configManager.Get(), outage.ConfigReloadResolverUser); err != nil {
		log.WithField("error", err).Error("Failed to resolve outages for sub-components removed from configuration")
	}
	configManager.OnUpdate(func(newConfig *types.DashboardConfig) {
		if err := outageManager.ResolveActiveOutagesForMissingSubComponents(newConfig, outage.ConfigReloadResolverUser); err != nil {
			log.WithField("error", err).Error("Failed to resolve outages after config reload")
		}
	})

	pingRepo := repositories.NewGORMComponentPingRepository(db)
	triageNoteRepo := repositories.NewGORMTriageNoteRepository(db)
	outageLinkRepo := repositories.NewGORMOutageLinkRepository(db)
	sloRepo := repositories.NewGORMSLOWorkspaceRepository(db)
	server := NewServer(configManager, log, opts.CORSOrigin, hmacSecret, groupCache, outageManager, pingRepo, triageNoteRepo, outageLinkRepo, sloRepo)

	absentReportChecker := NewAbsentMonitoredComponentReportChecker(configManager, outageManager, pingRepo, opts.AbsentReportCheckInterval, log)
	go absentReportChecker.Start(ctx)

	trtPayloadPruner := NewTRTSLOPayloadPruner(configManager, sloRepo, opts.TRTPayloadPruneInterval, log)
	go trtPayloadPruner.Start(ctx)

	suspectedExpiryChecker := NewSuspectedOutageExpiryChecker(outageManager, 30*time.Minute, log)
	go suspectedExpiryChecker.Start(ctx)

	addr := ":" + opts.Port
	go func() {
		if err := server.Start(addr); err != nil && err != http.ErrServerClosed {
			log.WithFields(logrus.Fields{
				"address": addr,
				"error":   err,
			}).Fatal("Server failed to start")
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, 5*time.Second)
	defer shutdownCancel()
	if err := server.Stop(shutdownCtx); err != nil {
		log.WithField("error", err).Error("Graceful shutdown failed")
	}
}
