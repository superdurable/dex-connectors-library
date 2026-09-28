// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command summarize-text runs the Worker for the Claude summarize-text example Flow.
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

	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	summarizetext "github.com/superdurable/dex-connectors-library/connectors/anthropic/examples/summarize-text/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("summarize-text stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := claude.NewLocalConnection(store, summarizetext.ConnectionName)
	if err != nil {
		return err
	}
	summaryModel, err := loadSummaryModelConfiguration(store)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{summarizetext.NewFlow(connection, summaryModel)})
	if err != nil {
		return fmt.Errorf("register Claude summary Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-claude-summarize-text-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8819"),
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("claude summary worker starting", "connection", summarizetext.ConnectionName, "summary_model", summaryModel.Model)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadSummaryModelConfiguration reads the SummarizeText Step's model pick. A
// Step that was never configured in Dex Web uses the connection's model.
func loadSummaryModelConfiguration(store *localconfig.Store) (summarizetext.SummaryModelConfiguration, error) {
	loaded, err := localconfig.LoadOperationConfiguration[summarizetext.SummaryModelConfiguration](
		store, summarizetext.SummaryModelConfigurationRef(),
	)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return summarizetext.SummaryModelConfiguration{}, nil
	}
	if err != nil {
		return summarizetext.SummaryModelConfiguration{}, err
	}
	return loaded.Value, nil
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
