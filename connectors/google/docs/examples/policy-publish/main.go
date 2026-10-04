// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command policy-publish runs the Worker for the Google Docs policy-publish example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	policypublish "github.com/superdurable/dex-connectors-library/connectors/google/docs/examples/policy-publish/flow"
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
		slog.Error("policy-publish stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := docs.NewProjectConnection(project, policypublish.ConnectionName)
	if err != nil {
		return err
	}
	template, err := loadOperationConfiguration[policypublish.TemplateConfiguration](project.Configuration, policypublish.TemplateConfigurationRef())
	if err != nil {
		return err
	}
	destinationFolder, err := loadOperationConfiguration[policypublish.FolderConfiguration](project.Configuration, policypublish.DestinationFolderConfigurationRef())
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{policypublish.NewFlow(connection, template, destinationFolder)})
	if err != nil {
		return fmt.Errorf("register Google Docs policy-publish Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-google-docs-policy-publish-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Google Docs policy-publish worker starting", "connection", policypublish.ConnectionName,
		"template_document_id", template.Value.DocumentID, "destination_folder_id", destinationFolder.Value.FolderID)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadOperationConfiguration reads a Step's saved pick once at startup. A Step
// without a saved pick uses a blank value, which the Flow documents.
func loadOperationConfiguration[T any](
	configuration projectconfig.Configuration,
	reference sdkgo.ConnectorConfigurationRef,
) (sdkgo.ConnectorLoadedConfiguration[T], error) {
	loaded, err := provider.LoadOperationConfiguration[T](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return sdkgo.ConnectorLoadedConfiguration[T]{Reference: reference}, nil
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
