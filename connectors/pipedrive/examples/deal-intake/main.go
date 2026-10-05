// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

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

	"github.com/superdurable/dex-connectors-library/connectors/pipedrive"
	dealintake "github.com/superdurable/dex-connectors-library/connectors/pipedrive/examples/deal-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK and any other library that logs to slog.Default() share the same handler.
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("deal-intake stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	isInvalidLevel := false
	if strings.TrimSpace(levelName) != "" {
		isInvalidLevel = level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if isInvalidLevel {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	settings, err := loadSettings(project.Configuration)
	if err != nil {
		return err
	}
	connection, err := pipedrive.NewProjectConnection(project, dealintake.ConnectionName)
	if err != nil {
		return err
	}
	flow, err := dealintake.NewFlow(connection, &settings)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Pipedrive deal intake Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-pipedrive-deal-intake-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8847"),
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	// The Client only checks Dex Server health; Dex Web Start Flow starts every run.
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"), WorkerTarget: worker.WorkerTarget(),
	})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	// Worker.Start fails at once when the Dex Server is unreachable, so wait for it first.
	if err := waitForDexServer(ctx, client.HealthCheck, logger); err != nil {
		if ctx.Err() != nil {
			// Control-C while waiting is a clean shutdown, not a failure.
			err = nil
		}
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	logger.Info("deal-intake worker started", "connection", dealintake.ConnectionName,
		"pipeline_id", settings.Deal.PipelineID, "stage_id", settings.Deal.StageID)
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
}

// loadSettings reads the Steps' picks once; an unsaved owner is blank, and the deal stage is required.
func loadSettings(configuration projectconfig.Configuration) (dealintake.Settings, error) {
	var settings dealintake.Settings
	leadOwner, err := provider.LoadOperationConfiguration[dealintake.LeadOwnerConfiguration](
		configuration, dealintake.LeadOwnerConfigurationRef(),
	)
	switch {
	case errors.Is(err, projectconfig.ErrObjectNotFound):
	case err != nil:
		return dealintake.Settings{}, fmt.Errorf("load the lead owner saved in Dex Web: %w", err)
	default:
		settings.LeadOwner = leadOwner.Value
	}
	deal, err := provider.LoadOperationConfiguration[dealintake.DealConfiguration](configuration, dealintake.DealConfigurationRef())
	if err != nil {
		return dealintake.Settings{}, fmt.Errorf("load the deal stage saved in Dex Web: %w", err)
	}
	settings.Deal = deal.Value
	return settings, nil
}

// waitForDexServer returns once the Dex Server answers a health check. It logs a WARN record for each
// failed check and retries with delays that grow from 250 milliseconds to 30 seconds, then logs an INFO
// record when a later check succeeds. It returns ctx.Err() when ctx ends first.
func waitForDexServer(ctx context.Context, healthCheck func(context.Context) (dex.HealthInfo, error), logger *slog.Logger) error {
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := healthCheck(attemptCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				logger.Info("dex server available after retry", "attempts", attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Warn("dex server unavailable; retrying", "attempt", attempt, "delay", delay, "error", err.Error())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(2*delay, 30*time.Second)
	}
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
