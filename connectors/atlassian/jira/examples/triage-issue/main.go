// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command triage-issue runs the Worker for the Jira issue triage example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	triageissue "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/examples/triage-issue/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("triage-issue stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := jira.NewProjectConnection(project, triageissue.ConnectionName)
	if err != nil {
		return err
	}
	selection, err := loadProjectSelection(project.Configuration)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{triageissue.NewFlow(connection, selection)})
	if err != nil {
		return fmt.Errorf("register Jira issue triage Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-jira-triage-issue-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Jira issue triage worker starting", "connection", triageissue.ConnectionName, "project_selected", selection.ProjectKey != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadProjectSelection reads the picked project once at startup; an unsaved picker uses each input's projectKey.
func loadProjectSelection(configuration projectconfig.Configuration) (triageissue.ProjectSelection, error) {
	loaded, err := provider.LoadOperationConfiguration[triageissue.ProjectSelection](configuration, triageissue.ProjectSelectionConfigurationRef())
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return triageissue.ProjectSelection{}, nil
	}
	if err != nil {
		return triageissue.ProjectSelection{}, err
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
