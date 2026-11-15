// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command file-intake runs the Worker and the durable Google Drive file Trigger runner for the
// file-intake example Flow. Every file created in the configured folder starts one Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/google/drive/go"
	fileintake "github.com/superdurable/dex-connectors-library/connectors/google/drive/go/examples/file-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("file-intake stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := drive.NewProjectConnection(project, fileintake.ConnectionName, drive.WithLogger(logger))
	if err != nil {
		return err
	}
	flow := fileintake.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Google Drive file-intake Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-google-drive-file-intake-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	defer cache.Close()
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	if err != nil {
		return err
	}
	defer client.Close()
	runner, err := drive.NewProjectFileTriggerRunner(project, fileintake.ConnectionName, drive.ProjectFileTriggerRunnerConfig{
		FileCreatedRoutes: []drive.ProjectFileTriggerRoute{{
			BindingName: fileintake.NewFileTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, flow, fileintake.AcceptCreatedFile, fileintake.ResolveFlowID,
				fileintake.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
		}},
	}, drive.WithLogger(logger))
	if err != nil {
		return err
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	if err != nil {
		return err
	}
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	logger.Info("Google Drive file-intake worker and Trigger runner starting", "connection", fileintake.ConnectionName)
	runnerCtx, stopRunner := context.WithCancel(ctx)
	defer stopRunner()
	runnerResult := make(chan error, 1)
	go func() { runnerResult <- runner.Run(runnerCtx) }()
	select {
	case <-ctx.Done():
		err = <-runnerResult
	case err = <-runnerResult:
	case err = <-workerResult:
	}
	if errors.Is(err, context.Canceled) {
		err = nil
	}
	return errors.Join(err, stopWorker(worker))
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
