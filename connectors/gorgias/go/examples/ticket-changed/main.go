// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command ticket-changed runs the Worker for the Gorgias ticketChanged example Flow and polls the
// helpdesk event log, starting one Flow per ticket event.
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

	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
	ticketchanged "github.com/superdurable/dex-connectors-library/connectors/gorgias/go/examples/ticket-changed/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Gorgias-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "GORGIAS_LOCAL_API_BASE_URL"

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("ticket-changed stopped", "error", err)
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
	options := connectionOptions(logger)
	connection, err := gorgias.NewProjectConnection(project, ticketchanged.ConnectionName, options...)
	if err != nil {
		return err
	}
	flow := ticketchanged.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Gorgias ticket change Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-gorgias-ticket-changed-blobs")), MaxBytes: 1 << 30,
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
	// Each event is recorded in the binding's project Trigger inbox before the event log position moves past it.
	triggerRunner, err := gorgias.NewProjectTicketTriggerRunner(project, ticketchanged.ConnectionName, gorgias.ProjectTicketTriggerRunnerConfig{
		TicketChangedRoutes: []gorgias.ProjectTicketChangedTriggerRoute{{
			BindingName: ticketchanged.TicketChangedTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(client, flow, ticketchanged.AcceptEveryTicketChange, ticketchanged.ResolveFlowID,
				ticketchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
		}},
	}, options...)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("Gorgias ticket change worker starting", "connection", ticketchanged.ConnectionName, "binding", ticketchanged.TicketChangedTriggerBinding)
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

// connectionOptions logs Trigger records to logger and redirects API calls only when the local-verification
// variable is set.
func connectionOptions(logger *slog.Logger) []gorgias.Option {
	options := []gorgias.Option{gorgias.WithLogger(logger)}
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		logger.Warn("Gorgias API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		options = append(options, gorgias.WithAPIBaseURL(baseURL))
	}
	return options
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
