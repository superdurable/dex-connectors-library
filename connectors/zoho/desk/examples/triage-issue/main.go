// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command triage-issue runs the Worker for the Zoho Desk triage-issue example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	triageissue "github.com/superdurable/dex-connectors-library/connectors/zoho/desk/examples/triage-issue/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Zoho Desk-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "ZOHO_DESK_LOCAL_API_BASE_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("triage-issue stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := desk.NewLocalConnection(store, triageissue.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{triageissue.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register Zoho Desk triage-issue Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-zoho-desk-triage-issue-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8856"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Zoho Desk triage-issue worker starting", "connection", triageissue.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects Zoho Desk API calls only when the local-verification variable is set.
func connectionOptions() []desk.Option {
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		slog.Warn("Zoho Desk API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		return []desk.Option{desk.WithAPIBaseURL(baseURL)}
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
