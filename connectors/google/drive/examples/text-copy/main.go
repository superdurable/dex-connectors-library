// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command text-copy runs the Worker for the Google Drive text-copy example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/google/drive"
	textcopy "github.com/superdurable/dex-connectors-library/connectors/google/drive/examples/text-copy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("text-copy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := drive.NewLocalConnection(store, textcopy.ConnectionName)
	if err != nil {
		return err
	}
	sourceFolder, err := loadFolderConfiguration(store, textcopy.SourceFolderConfigurationRef())
	if err != nil {
		return err
	}
	destinationFolder, err := loadFolderConfiguration(store, textcopy.DestinationFolderConfigurationRef())
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{textcopy.NewFlow(connection, sourceFolder, destinationFolder)})
	if err != nil {
		return fmt.Errorf("register Google Drive text-copy Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-google-drive-text-copy-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8822"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Google Drive text-copy worker starting", "connection", textcopy.ConnectionName,
		"source_folder_id", sourceFolder.Value.FolderID, "destination_folder_id", destinationFolder.Value.FolderID)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadFolderConfiguration reads a Step's saved folder pick once at startup. A
// Step without a saved pick uses a blank folder, which the Flow documents.
func loadFolderConfiguration(
	store *localconfig.Store,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[textcopy.FolderConfiguration], error) {
	loaded, err := localconfig.LoadOperationConfiguration[textcopy.FolderConfiguration](store, reference)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[textcopy.FolderConfiguration]{Reference: reference}, nil
	}
	return loaded, err
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
