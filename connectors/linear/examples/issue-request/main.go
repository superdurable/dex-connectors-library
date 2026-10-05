// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command issue-request runs the Worker for the Linear issue request example Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issuerequest "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-request/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIURLEnvironmentVariable points the example at a local Linear-compatible fake for verification only.
const localAPIURLEnvironmentVariable = "LINEAR_LOCAL_API_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("issue-request stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := linear.NewProjectConnection(project, issuerequest.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	selection, err := loadTeamSelection(project.Configuration)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{issuerequest.NewFlow(connection, selection)})
	if err != nil {
		return fmt.Errorf("register Linear issue request Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-linear-issue-request-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Linear issue request worker starting", "connection", issuerequest.ConnectionName, "team_selected", selection.TeamID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadTeamSelection reads the picked team once at startup; an unsaved picker uses each input's teamId.
func loadTeamSelection(configuration projectconfig.Configuration) (issuerequest.TeamSelection, error) {
	loaded, err := provider.LoadOperationConfiguration[issuerequest.TeamSelection](configuration, issuerequest.TeamSelectionConfigurationRef())
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		return issuerequest.TeamSelection{}, nil
	}
	if err != nil {
		return issuerequest.TeamSelection{}, err
	}
	return loaded.Value, nil
}

// connectionOptions redirects API calls only when the local-verification variable is set.
func connectionOptions() []linear.Option {
	if apiURL := os.Getenv(localAPIURLEnvironmentVariable); apiURL != "" {
		slog.Warn("Linear API requests go to a local URL", "variable", localAPIURLEnvironmentVariable)
		return []linear.Option{linear.WithAPIURL(apiURL)}
	}
	return nil
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
