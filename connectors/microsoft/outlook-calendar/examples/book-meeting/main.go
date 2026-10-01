// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command book-meeting runs the Worker for the Outlook Calendar example Flow.
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

	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	bookmeeting "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar/examples/book-meeting/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localProviderURLEnvironmentVariable redirects Graph and token requests to a loopback fake for local verification only.
const localProviderURLEnvironmentVariable = "OUTLOOK_CALENDAR_LOCAL_PROVIDER_URL"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		slog.Error("book-meeting stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := outlookcalendar.NewLocalConnection(store, bookmeeting.ConnectionName, connectionOptions()...)
	if err != nil {
		return err
	}
	selection, err := loadCalendarSelection(store)
	if err != nil {
		return err
	}
	registry, err := dex.NewRegistry([]dex.Flow{bookmeeting.NewFlow(connection, selection)})
	if err != nil {
		return fmt.Errorf("register Outlook Calendar meeting Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-outlook-calendar-book-meeting-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8839"), FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	slog.Info("Outlook Calendar meeting worker starting", "connection", bookmeeting.ConnectionName, "calendar_selected", selection.CalendarID != "")
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), cache.Close())
}

// connectionOptions redirects Microsoft requests only when the local-verification variable is set.
func connectionOptions() []outlookcalendar.Option {
	if providerURL := os.Getenv(localProviderURLEnvironmentVariable); providerURL != "" {
		slog.Warn("Microsoft Graph and token requests go to a local URL", "variable", localProviderURLEnvironmentVariable)
		return []outlookcalendar.Option{outlookcalendar.WithLocalProviderURL(providerURL)}
	}
	return nil
}

// loadCalendarSelection reads the picked calendar once at startup; an unsaved picker uses the default calendar.
func loadCalendarSelection(store *localconfig.Store) (bookmeeting.CalendarSelection, error) {
	loaded, err := localconfig.LoadOperationConfiguration[bookmeeting.CalendarSelection](store, bookmeeting.CalendarSelectionConfigurationRef())
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		return bookmeeting.CalendarSelection{}, nil
	}
	if err != nil {
		return bookmeeting.CalendarSelection{}, err
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
