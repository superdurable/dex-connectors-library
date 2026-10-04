// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command support-reply runs the Worker for the Outlook Mail support-reply example Flow.
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

	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	supportreply "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/examples/support-reply/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localProviderURLEnvironmentVariable routes Microsoft requests to a loopback Graph fake, for local verification only.
const localProviderURLEnvironmentVariable = "OUTLOOK_MAIL_LOCAL_PROVIDER_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("support-reply stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := outlookmail.NewProjectConnection(project, supportreply.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	archiveFolder, err := loadArchiveFolderSelection(project.Configuration)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{supportreply.NewFlow(connection, archiveFolder)})
	if err != nil {
		return fmt.Errorf("register Outlook Mail support-reply Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-outlook-mail-support-reply-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8837"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Outlook Mail support-reply worker starting", "connection", supportreply.ConnectionName, "archive_folder_picked", archiveFolder.FolderID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadArchiveFolderSelection reads the picked folder once at startup; an unsaved picker uses the Archive folder.
func loadArchiveFolderSelection(configuration projectconfig.Configuration) (supportreply.ArchiveFolderSelection, error) {
	loaded, err := provider.LoadOperationConfiguration[supportreply.ArchiveFolderSelection](configuration, supportreply.ArchiveFolderConfigurationRef())
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return supportreply.ArchiveFolderSelection{}, nil
	}
	if err != nil {
		return supportreply.ArchiveFolderSelection{}, err
	}
	return loaded.Value, nil
}

// connectionOptions routes Microsoft requests to a loopback fake when OUTLOOK_MAIL_LOCAL_PROVIDER_URL is set.
func connectionOptions() []outlookmail.Option {
	providerURL := os.Getenv(localProviderURLEnvironmentVariable)
	if providerURL == "" {
		return nil
	}
	slog.Warn("Microsoft requests go to a local Graph stand-in", "variable", localProviderURLEnvironmentVariable)
	return []outlookmail.Option{outlookmail.WithLocalProviderURL(providerURL)}
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
