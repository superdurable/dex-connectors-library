// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command schedule-meeting runs the Worker for the Google Calendar example Flow.
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

	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	schedulemeeting "github.com/superdurable/dex-connectors-library/connectors/google/calendar/examples/schedule-meeting/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("schedule-meeting stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := calendar.NewLocalConnection(store, schedulemeeting.ConnectionName)
	if err != nil {
		return err
	}
	selection, err := loadCalendarSelection(store)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{schedulemeeting.NewFlow(connection, selection)})
	if err != nil {
		return fmt.Errorf("register Google Calendar meeting Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-google-calendar-schedule-meeting-blobs")), MaxBytes: 1 << 30,
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
	slog.Info("Google Calendar meeting worker starting", "connection", schedulemeeting.ConnectionName, "calendar_selected", selection.CalendarID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// loadCalendarSelection reads the picked calendar once at startup; an unsaved picker uses the primary calendar.
func loadCalendarSelection(store *localconfig.Store) (schedulemeeting.CalendarSelection, error) {
	loaded, err := localconfig.LoadOperationConfiguration[schedulemeeting.CalendarSelection](store, schedulemeeting.CalendarSelectionConfigurationRef())
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return schedulemeeting.CalendarSelection{}, nil
	}
	if err != nil {
		return schedulemeeting.CalendarSelection{}, err
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
