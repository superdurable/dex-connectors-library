// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command text-copy runs the Worker for the OneDrive and SharePoint text-copy example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive"
	textcopy "github.com/superdurable/dex-connectors-library/connectors/microsoft/onedrive/examples/text-copy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
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
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := onedrive.NewProjectConnection(project, textcopy.ConnectionName)
	if err != nil {
		return err
	}
	sourceLocation, err := loadLocationConfiguration(project.Configuration, textcopy.SourceLocationConfigurationRef())
	if err != nil {
		return err
	}
	destinationLocation, err := loadLocationConfiguration(project.Configuration, textcopy.DestinationLocationConfigurationRef())
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{textcopy.NewFlow(connection, sourceLocation, destinationLocation)})
	if err != nil {
		return fmt.Errorf("register OneDrive text-copy Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-onedrive-text-copy-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8823"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("OneDrive text-copy worker starting", "connection", textcopy.ConnectionName,
		"source_drive_id", sourceLocation.Value.DriveID, "source_folder_id", sourceLocation.Value.FolderID,
		"destination_drive_id", destinationLocation.Value.DriveID, "destination_folder_id", destinationLocation.Value.FolderID)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadLocationConfiguration reads a Step's saved location picks once at startup. A
// Step without saved picks uses the signed-in user's OneDrive root, which the Flow documents.
func loadLocationConfiguration(
	configuration projectconfig.Configuration,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[textcopy.LocationConfiguration], error) {
	loaded, err := provider.LoadOperationConfiguration[textcopy.LocationConfiguration](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[textcopy.LocationConfiguration]{Reference: reference}, nil
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
