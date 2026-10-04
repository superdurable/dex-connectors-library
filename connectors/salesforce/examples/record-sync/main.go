// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command record-sync runs the Worker for the Salesforce record-sync example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	recordsync "github.com/superdurable/dex-connectors-library/connectors/salesforce/examples/record-sync/flow"
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
		slog.Error("record-sync stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := salesforce.NewProjectConnection(project, recordsync.ConnectionName)
	if err != nil {
		return err
	}
	configuration, err := loadSyncConfiguration(project.Configuration)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{recordsync.NewFlow(connection, configuration)})
	if err != nil {
		return fmt.Errorf("register Salesforce record-sync Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-salesforce-record-sync-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8827"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Salesforce record-sync worker starting", "connection", recordsync.ConnectionName,
		"sobject_type", configuration.Value.SObjectType, "match_field", configuration.Value.MatchField,
		"external_id_field", configuration.Value.ExternalIDField)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadSyncConfiguration reads the FindMatchingRecords Step's saved units once
// at startup. A Step without saved units uses blank values, which the Flow documents.
func loadSyncConfiguration(configuration projectconfig.Configuration) (sdkgo.ConnectorLoadedConfiguration[recordsync.SyncConfiguration], error) {
	reference := recordsync.SyncConfigurationRef()
	loaded, err := provider.LoadOperationConfiguration[recordsync.SyncConfiguration](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[recordsync.SyncConfiguration]{Reference: reference}, nil
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
