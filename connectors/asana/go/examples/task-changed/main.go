// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command task-changed runs the Worker for the Asana taskChanged example Flow and polls the
// picked project, starting one Flow per task added, changed, removed, deleted, or restored.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	asana "github.com/superdurable/dex-connectors-library/connectors/asana/go"
	taskchanged "github.com/superdurable/dex-connectors-library/connectors/asana/go/examples/task-changed/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("task-changed stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	isInvalid := strings.TrimSpace(levelName) != "" && level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if isInvalid {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := asana.NewProjectConnection(project, taskchanged.ConnectionName, asana.WithLogger(logger))
	if err != nil {
		return err
	}
	flow := taskchanged.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Asana task change Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-asana-task-changed-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	// Each task event is recorded in the binding's project Trigger inbox before the sync token moves past it.
	triggerRunner, err := asana.NewProjectTaskTriggerRunner(project, taskchanged.ConnectionName, asana.ProjectTaskTriggerRunnerConfig{
		TaskChangedRoutes: []asana.ProjectTaskChangedTriggerRoute{{
			BindingName: taskchanged.TaskChangedTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, flow, taskchanged.AcceptEveryTaskChange, taskchanged.ResolveFlowID,
				taskchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
		}},
	}, asana.WithLogger(logger))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("Asana task change worker starting", "connection", taskchanged.ConnectionName, "binding", taskchanged.TaskChangedTriggerBinding)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	triggerResult := make(chan error, 1)
	triggerContext, cancelTrigger := context.WithCancel(ctx)
	defer cancelTrigger()
	go func() { triggerResult <- triggerRunner.Run(triggerContext) }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	case err = <-triggerResult:
		// Refill the channel so the shutdown read below does not block.
		triggerResult <- nil
	}
	cancelTrigger()
	if triggerErr := <-triggerResult; triggerErr != nil && !errors.Is(triggerErr, context.Canceled) {
		err = errors.Join(err, triggerErr)
	}
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
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
