// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command work-request runs the Worker for the monday.com work-request example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/monday"
	workrequest "github.com/superdurable/dex-connectors-library/connectors/monday/examples/work-request/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIURLEnvironmentVariable points the example at a local monday.com-compatible fake for verification only.
const localAPIURLEnvironmentVariable = "MONDAY_LOCAL_API_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("work-request stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := monday.NewLocalConnection(store, workrequest.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{workrequest.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register monday.com work-request Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-monday-work-request-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8834"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("monday.com work-request worker starting", "connection", workrequest.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects API calls only when the local-verification variable is set.
func connectionOptions() []monday.Option {
	if apiURL := os.Getenv(localAPIURLEnvironmentVariable); apiURL != "" {
		slog.Warn("monday.com API requests go to a local URL", "variable", localAPIURLEnvironmentVariable)
		return []monday.Option{monday.WithAPIURL(apiURL)}
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
