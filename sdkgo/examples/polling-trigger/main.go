// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command polling-trigger starts one Dex Flow for every change a fake change-log provider reports, using
// the durable poll Trigger kit. Run it beside `dexcli dev`; it appends a provider change every few seconds.
// With DEX_CONNECTOR_CONFIG_FILE or hosted project variables set, the cursor and the Trigger inbox live in
// project storage; otherwise the cursor is in memory and a restart reseeds.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("polling-trigger stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	changeLog, server := startChangeLogProvider()
	defer server.Close()
	go appendChanges(ctx, changeLog)
	flow := &itemSyncFlow{}
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return err
	}
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(os.TempDir(), "dex-polling-trigger-blobs"), MaxBytes: 64 << 20})
	if err != nil {
		return err
	}
	defer cache.Close()
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	if err != nil {
		return err
	}
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("worker stopped", "error", errors.Join(worker.Stop(stopCtx), <-workerResult))
	}()
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	if err != nil {
		return err
	}
	defer client.Close()
	api := &changeLogClient{baseURL: server.URL, httpClient: http.DefaultClient}
	return runItemTrigger(ctx, loadOptionalProject(ctx, logger), client, flow, api, logger)
}

// loadOptionalProject returns nil when no project configuration is selected.
func loadOptionalProject(ctx context.Context, logger *slog.Logger) *projectconfig.LoadedProject {
	if os.Getenv(projectconfig.LocalConnectionsFileEnvironment) == "" && os.Getenv("DEX_PROJECT_ID") == "" {
		logger.Warn("no project configuration; the poll cursor is kept in memory and a restart reseeds")
		return nil
	}
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		logger.Error("project configuration unavailable; the poll cursor is kept in memory", "error", err)
		return nil
	}
	return project
}

func appendChanges(ctx context.Context, changeLog *changeLogProvider) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for sequence := 1; ; sequence++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changeLog.addChange("item-"+strconv.Itoa(sequence%3), fmt.Sprintf("revision %d", sequence))
		}
	}
}
