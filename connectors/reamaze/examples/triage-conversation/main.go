// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command triage-conversation runs the Worker for the Re:amaze triage-conversation example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	triageconversation "github.com/superdurable/dex-connectors-library/connectors/reamaze/examples/triage-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Re:amaze-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "REAMAZE_LOCAL_API_BASE_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("triage-conversation stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := reamaze.NewProjectConnection(project, triageconversation.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{triageconversation.NewFlow(connection)})
	if err != nil {
		return fmt.Errorf("register Re:amaze triage-conversation Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-reamaze-triage-conversation-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Re:amaze triage-conversation worker starting", "connection", triageconversation.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects API calls only when the local-verification variable is set.
func connectionOptions() []reamaze.Option {
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		slog.Warn("Re:amaze API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		return []reamaze.Option{reamaze.WithAPIBaseURL(baseURL)}
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
