// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command customer-issue runs the Worker for the Zendesk Support customer-issue example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	customerissue "github.com/superdurable/dex-connectors-library/connectors/zendesk/support/examples/customer-issue/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Zendesk-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "ZENDESK_SUPPORT_LOCAL_API_BASE_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("customer-issue stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := support.NewProjectConnection(project, customerissue.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{customerissue.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register Zendesk customer-issue Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-zendesk-customer-issue-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8830"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Zendesk customer-issue worker starting", "connection", customerissue.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects API calls only when the local-verification variable is set.
func connectionOptions() []support.Option {
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		slog.Warn("Zendesk API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		return []support.Option{support.WithAPIBaseURL(baseURL)}
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
