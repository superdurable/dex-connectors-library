// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command conversation-triage runs the Worker for the Front conversation triage example Flow.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/front"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/front/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Front-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "FRONT_LOCAL_API_BASE_URL"

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("conversation-triage stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records at LOG_LEVEL: debug, info, warn, or error, with info by default.
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	isInvalid := strings.TrimSpace(levelName) != "" && level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if isInvalid {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := front.NewProjectConnection(project, conversationtriage.ConnectionName, localAPIOptions(logger)...)
	if err != nil {
		return err
	}
	search, err := loadSearchConfiguration(project.Configuration)
	if err != nil {
		return err
	}
	routing, err := loadRoutingConfiguration(project.Configuration, logger)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{conversationtriage.NewFlow(connection, search, routing)})
	if err != nil {
		return fmt.Errorf("register Front conversation triage Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-front-conversation-triage-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8861"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	logger.Info("Front conversation triage worker starting", "connection", conversationtriage.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadSearchConfiguration treats an unsaved inboxPicker as every inbox.
func loadSearchConfiguration(configuration projectconfig.Configuration) (sdkgo.ConnectorLoadedConfiguration[conversationtriage.SearchConfiguration], error) {
	reference := conversationtriage.SearchConfigurationRef()
	loaded, err := provider.LoadOperationConfiguration[conversationtriage.SearchConfiguration](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[conversationtriage.SearchConfiguration]{Reference: reference}, nil
	}
	return loaded, err
}

// loadRoutingConfiguration treats unsaved pickers as empty, which fails each Flow with guidance instead of the Worker.
func loadRoutingConfiguration(
	configuration projectconfig.Configuration, logger *slog.Logger,
) (sdkgo.ConnectorLoadedConfiguration[conversationtriage.RoutingConfiguration], error) {
	reference := conversationtriage.RoutingConfigurationRef()
	loaded, err := provider.LoadOperationConfiguration[conversationtriage.RoutingConfiguration](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		logger.Warn("the triage tag is not configured; choose it in Dex Web and restart", "step", reference.StepType)
		return sdkgo.ConnectorLoadedConfiguration[conversationtriage.RoutingConfiguration]{Reference: reference}, nil
	}
	return loaded, err
}

// localAPIOptions redirects API calls only when the local-verification variable is set.
func localAPIOptions(logger *slog.Logger) []front.Option {
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		logger.Warn("Front API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		return []front.Option{front.WithAPIBaseURL(baseURL)}
	}
	return nil
}

func stopWorker(worker *dex.Worker) error {
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return worker.Stop(stopCtx)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
