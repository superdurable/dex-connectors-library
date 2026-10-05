// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command support-request runs the Worker for the Jira Service Management support request example Flow.
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

	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	supportrequest "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management/examples/support-request/flow"
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
		slog.Error("support-request stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := jiraservicemanagement.NewProjectConnection(project, supportrequest.ConnectionName)
	if err != nil {
		return err
	}
	desk, err := loadSelection[supportrequest.DeskSelection](project.Configuration, supportrequest.DeskSelectionConfigurationRef())
	if err != nil {
		return err
	}
	requestType, err := loadSelection[supportrequest.RequestTypeSelection](project.Configuration, supportrequest.RequestTypeSelectionConfigurationRef())
	if err != nil {
		return err
	}
	if err := supportrequest.ValidateSelections(desk, requestType); err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{supportrequest.NewFlow(connection, desk, requestType)})
	if err != nil {
		return fmt.Errorf("register Jira Service Management support request Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-jsm-support-request-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Jira Service Management support request worker starting", "connection", supportrequest.ConnectionName,
		"service_desk_selected", desk.ServiceDeskID != "", "request_type_selected", requestType.RequestTypeID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadSelection reads one picker value once at startup; an unsaved picker uses each Start Flow input's IDs.
func loadSelection[T any](configuration projectconfig.Configuration, reference sdkgo.ConnectorConfigurationRef) (T, error) {
	loaded, err := provider.LoadOperationConfiguration[T](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		var unsaved T
		return unsaved, nil
	}
	if err != nil {
		var unsaved T
		return unsaved, err
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
