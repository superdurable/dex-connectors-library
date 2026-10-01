// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command approved-order runs the Worker for the Xero approved-order example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/xero"
	approvedorder "github.com/superdurable/dex-connectors-library/connectors/xero/examples/approved-order/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localProviderURLEnvironmentVariable points the example at a local Xero-compatible fake for verification only.
const localProviderURLEnvironmentVariable = "XERO_LOCAL_PROVIDER_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("approved-order stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := xero.NewLocalConnection(store, approvedorder.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{approvedorder.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register Xero approved-order Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-xero-approved-order-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8836"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Xero approved-order worker starting", "connection", approvedorder.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects Xero API and token requests only when the local-verification variable is set.
func connectionOptions() []xero.Option {
	if providerURL := os.Getenv(localProviderURLEnvironmentVariable); providerURL != "" {
		slog.Warn("Xero API and token requests go to a local URL", "variable", localProviderURLEnvironmentVariable)
		return []xero.Option{xero.WithLocalProviderURL(providerURL)}
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
