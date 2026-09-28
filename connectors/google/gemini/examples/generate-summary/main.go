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

	gemini "github.com/superdurable/dex-connectors-library/connectors/google/gemini"
	generatesummary "github.com/superdurable/dex-connectors-library/connectors/google/gemini/examples/generate-summary/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("generate-summary stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	isLevelInvalid := false
	if strings.TrimSpace(levelName) != "" {
		isLevelInvalid = level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if isLevelInvalid {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := gemini.NewLocalConnection(store, generatesummary.ConnectionName)
	if err != nil {
		return err
	}
	summaryModel, err := loadSummaryModelConfiguration(store)
	if err != nil {
		return err
	}
	flow := generatesummary.NewFlow(connection, summaryModel)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Gemini summary Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-gemini-generate-summary-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8815"),
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
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
	logger.Info("gemini summary worker starting", "connection", generatesummary.ConnectionName, "summary_model", describeSummaryModel(summaryModel))
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
}

// waitForDexServer retries failed health checks with a WARN log, backing off from 250 ms to 30 s.
// It returns nil once the Dex Server answers, or ctx.Err() when ctx ends first.
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

// loadSummaryModelConfiguration reads the GenerateSummary Step's model pick. A
// Step that was never configured in Dex Web uses the connection's model.
func loadSummaryModelConfiguration(store *localconfig.Store) (generatesummary.SummaryModelConfiguration, error) {
	loaded, err := localconfig.LoadOperationConfiguration[generatesummary.SummaryModelConfiguration](
		store, generatesummary.SummaryModelConfigurationRef(),
	)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return generatesummary.SummaryModelConfiguration{}, nil
	}
	if err != nil {
		return generatesummary.SummaryModelConfiguration{}, err
	}
	return loaded.Value, nil
}

// describeSummaryModel names the model for the startup log; an empty pick uses the connection's model.
func describeSummaryModel(summaryModel generatesummary.SummaryModelConfiguration) string {
	if summaryModel.Model == "" {
		return "connection model"
	}
	return summaryModel.Model
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
