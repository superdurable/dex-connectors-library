// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command publish-policy runs the Worker for the Confluence policy publication example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence"
	publishpolicy "github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/examples/publish-policy/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("publish-policy stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := confluence.NewLocalConnection(store, publishpolicy.ConnectionName)
	if err != nil {
		return err
	}
	selection, err := loadSpaceSelection(store)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{publishpolicy.NewFlow(connection, selection)})
	if err != nil {
		return fmt.Errorf("register Confluence policy publication Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-confluence-publish-policy-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Confluence policy publication worker starting", "connection", publishpolicy.ConnectionName, "space_selected", selection.SpaceKey != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadSpaceSelection reads the picked space once at startup; an unsaved picker uses each input's spaceKey.
func loadSpaceSelection(store *localconfig.Store) (publishpolicy.SpaceSelection, error) {
	loaded, err := localconfig.LoadOperationConfiguration[publishpolicy.SpaceSelection](store, publishpolicy.SpaceSelectionConfigurationRef())
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return publishpolicy.SpaceSelection{}, nil
	}
	if err != nil {
		return publishpolicy.SpaceSelection{}, err
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
