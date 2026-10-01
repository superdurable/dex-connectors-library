// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command response-recorder runs the Worker for the Google Forms response-recorder example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/google/forms"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/google/forms/examples/response-recorder/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("response-recorder stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := forms.NewLocalConnection(store, responserecorder.ConnectionName)
	if err != nil {
		return err
	}
	form, err := loadFormConfiguration(store, responserecorder.FormConfigurationRef())
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{responserecorder.NewFlow(connection, form)})
	if err != nil {
		return fmt.Errorf("register Google Forms response-recorder Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-google-forms-response-recorder-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Google Forms response-recorder worker starting", "connection", responserecorder.ConnectionName, "form_id", form.Value.FormID)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadFormConfiguration reads the saved form pick once; without one, each run fails at its first Step.
func loadFormConfiguration(
	store *localconfig.Store,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[responserecorder.FormConfiguration], error) {
	loaded, err := localconfig.LoadOperationConfiguration[responserecorder.FormConfiguration](store, reference)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[responserecorder.FormConfiguration]{Reference: reference}, nil
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
