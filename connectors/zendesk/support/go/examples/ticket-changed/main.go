// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command ticket-changed polls the Zendesk incremental ticket export with the ticketChanged Trigger and
// runs the Worker that records each changed ticket in its own Flow.
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

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	ticketchanged "github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/examples/ticket-changed/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the example at a local Zendesk-compatible fake for verification only.
const localAPIBaseURLEnvironmentVariable = "ZENDESK_SUPPORT_LOCAL_API_BASE_URL"

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the poll runner, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("ticket-changed stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records at LOG_LEVEL: debug, info, warn, or error, with info by default.
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
	connection, err := support.NewProjectConnection(project, ticketchanged.ConnectionName, options...)
	if err != nil {
		return err
	}
	flow := ticketchanged.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Zendesk ticket change Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-zendesk-ticket-changed-blobs")), MaxBytes: 1 << 30,
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
	// Each change is recorded in the binding's project Trigger inbox before the export cursor moves past it.
	triggerRunner, err := support.NewProjectTicketTriggerRunner(project, ticketchanged.ConnectionName, support.ProjectTicketTriggerRunnerConfig{
		TicketChangedRoutes: []support.ProjectTicketChangedTriggerRoute{{
			BindingName: ticketchanged.TicketChangedTriggerBinding, Target: newTicketChangeTarget(client, flow, logger),
		}},
	}, options...)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	if err := waitForDexServer(ctx, client.HealthCheck, logger); err != nil {
		if ctx.Err() != nil {
			// Control-C while waiting is a clean shutdown, not a failure.
			err = nil
		}
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runResults := make(chan error, 2)
	go func() { runResults <- worker.Start() }()
	go func() { runResults <- triggerRunner.Run(runCtx) }()
	select {
	case <-ctx.Done():
	case err = <-runResults:
	}
	cancel()
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		// Control-C during a long startup replay is a clean shutdown, not a failure.
		err = nil
	}
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
}

// newTicketChangeTarget starts one Flow per ticket change, with the Trigger event ID as the Flow ID suffix.
func newTicketChangeTarget(client *dex.Client, flow *ticketchanged.Flow, logger *slog.Logger) sdkgo.TriggerTarget[support.TicketChangedEvent] {
	bindingLogger := logger.With("connector", support.ConnectorID, "connection", ticketchanged.ConnectionName,
		"trigger", support.TicketChangedTriggerDefinition.Trigger.TriggerName, "binding", ticketchanged.TicketChangedTriggerBinding)
	return sdkgo.NewDexFlowTriggerTarget(client, flow, ticketchanged.AcceptEveryTicketChange, ticketchanged.ResolveFlowID,
		ticketchanged.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger))
}

// connectionOptions sends Trigger records to logger and redirects API calls only when the
// local-verification variable is set.
func connectionOptions(logger *slog.Logger) []support.Option {
	options := []support.Option{support.WithLogger(logger)}
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		logger.Warn("Zendesk API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		options = append(options, support.WithAPIBaseURL(baseURL))
	}
	return options
}

// waitForDexServer returns once the Dex Server answers a health check, retrying with delays that grow from
// 250 milliseconds to 30 seconds. It returns ctx.Err() when ctx ends first.
func waitForDexServer(ctx context.Context, healthCheck func(context.Context) (dex.HealthInfo, error), logger *slog.Logger) error {
	delay := 250 * time.Millisecond
	for attempt := 1; ; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := healthCheck(attemptCtx)
		cancel()
		if err == nil {
			if attempt > 1 {
				logger.Info("dex server available after retry", "attempts", attempt)
			}
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Warn("dex server unavailable; retrying", "attempt", attempt, "delay", delay, "error", err.Error())
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = min(2*delay, 30*time.Second)
	}
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
