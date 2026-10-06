// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/newsapi"
	companynews "github.com/superdurable/dex-connectors-library/connectors/newsapi/examples/company-news/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, slog.Default()); err != nil {
		slog.Error("company-news stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := newsapi.NewProjectConnection(project, companynews.ConnectionName)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{companynews.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register NewsAPI company news Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-newsapi-company-news-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8873"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
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
	logger.Info("NewsAPI company news worker starting", "connection", companynews.ConnectionName)
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
