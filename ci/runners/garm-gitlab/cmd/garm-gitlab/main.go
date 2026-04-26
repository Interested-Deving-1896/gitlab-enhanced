// garm-gitlab manages Incus runner instances for GitLab CI.
//
// It listens for GitLab job webhook events and scales Incus container/VM
// pools up and down to match demand, following the same pool-based lifecycle
// model as cloudbase/garm but targeting GitLab CI and Incus.
//
// Usage:
//
//	garm-gitlab -config /etc/garm-gitlab/config.toml
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"

	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/config"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/gitlab"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/pool"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/provider"
	"gitlab.com/openos-project/git-management_deving/gitlab-enhanced/ci/runners/garm-gitlab/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "garm-gitlab: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath  := flag.String("config",   "/etc/garm-gitlab/config.toml",      "path to TOML config file")
	logLevel := flag.String("log-level", "info",                              "log level: debug, info, warn, error")
	storePath := flag.String("store",   "/var/lib/garm-gitlab/state.db",     "path to SQLite state database")
	flag.Parse()

	log := logrus.New()
	log.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	lvl, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level %q: %w", *logLevel, err)
	}
	log.SetLevel(lvl)

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log.WithField("config", *cfgPath).Info("configuration loaded")

	// Connect to the local Incus daemon.
	incusProv, err := provider.NewIncusClient(log)
	if err != nil {
		return fmt.Errorf("connect to Incus: %w", err)
	}

	log.Info("connected to Incus daemon")

	// SQLite state store — persists instance state across restarts.
	st, err := store.Open(*storePath)
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	defer st.Close()

	log.WithField("path", *storePath).Info("state store opened")

	// GitLab API client (for runner registration/deregistration).
	gitlabClient := gitlab.NewClient(cfg.GitLab.URL, cfg.GitLab.Token, log)

	// Webhook event channel — the listener writes, the pool manager reads.
	eventCh := make(chan gitlab.JobEvent, 64)

	// Pool manager.
	mgr, err := pool.NewManager(cfg.Pools, gitlabClient, incusProv, st, eventCh, log)
	if err != nil {
		return fmt.Errorf("create pool manager: %w", err)
	}

	// Webhook listener.
	listener := gitlab.NewWebhookListener(cfg.API.ListenAddress, cfg.API.WebhookSecret, eventCh, log)

	// Run everything under a shared context that cancels on SIGINT/SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		log.WithField("address", cfg.API.ListenAddress).Info("starting webhook listener")
		return listener.ListenAndServe(ctx)
	})

	g.Go(func() error {
		log.Info("starting pool manager")
		return mgr.Run(ctx)
	})

	if err := g.Wait(); err != nil && err != context.Canceled {
		return err
	}

	log.Info("shutdown complete")
	return nil
}
