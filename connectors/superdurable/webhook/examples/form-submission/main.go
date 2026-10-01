// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command form-submission serves the webhook connector's endpoint and runs the Worker that records
// and forwards each verified form submission.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// submissionPath is where senders POST; expose it through an HTTPS tunnel in local development.
	submissionPath = "/webhooks/form-submission"
	readinessPath  = "/readyz"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the webhook endpoint, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("form-submission stopped", "error", err)
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

// run serves the webhook endpoint at once, so submissions are recorded even while Dex is unreachable.
func run(ctx context.Context, logger *slog.Logger, connectionOptions ...webhook.Option) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := webhook.NewLocalConnection(store, formsubmission.ConnectionName, connectionOptions...)
	if err != nil {
		return err
	}
	flow := formsubmission.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register form submission Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-webhook-form-submission-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8841"),
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"), WorkerTarget: worker.WorkerTarget(),
	})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	endpointRunner, err := newSubmissionEndpointRunner(store, client, flow, logger, connectionOptions)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	listener, err := net.Listen("tcp", environmentOr("WEBHOOK_BIND_ADDRESS", "127.0.0.1:8842"))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("form-submission webhook listening", "address", listener.Addr().String(), "path", submissionPath)
	server := &http.Server{Handler: newWebhookMux(endpointRunner), ReadHeaderTimeout: 5 * time.Second}
	runErr := runUntilStopped(ctx, server, listener, endpointRunner, worker, client.HealthCheck, logger)
	return errors.Join(runErr, client.Close(), cache.Close())
}

// newSubmissionEndpointRunner starts one Flow per accepted submission, with the event ID as request ID.
func newSubmissionEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *formsubmission.Flow, logger *slog.Logger, connectionOptions []webhook.Option,
) (*webhook.RequestReceivedEndpointRunner, error) {
	bindingLogger := logger.With("connector", webhook.ConnectorID, "connection", formsubmission.ConnectionName,
		"trigger", webhook.RequestReceivedTriggerDefinition.Trigger.TriggerName, "binding", formsubmission.SubmissionTriggerBinding)
	return webhook.NewLocalRequestReceivedEndpointRunner(store, formsubmission.ConnectionName, []webhook.LocalRequestReceivedTriggerRoute{{
		BindingName: formsubmission.SubmissionTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, formsubmission.AcceptSubmission, formsubmission.ResolveFlowID,
			formsubmission.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, append(slices.Clone(connectionOptions), webhook.WithLogger(logger))...)
}

// newWebhookMux mounts the endpoint and a readiness check that passes once the binding receives events.
func newWebhookMux(endpointRunner *webhook.RequestReceivedEndpointRunner) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(submissionPath, endpointRunner)
	mux.HandleFunc(readinessPath, func(response http.ResponseWriter, _ *http.Request) {
		if endpointRunner.RunningSourceCount() == 0 {
			http.Error(response, "webhook binding is not receiving yet", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(response, "ready\n") // The status already answered the check.
	})
	return mux
}

// runUntilStopped starts the Worker only from this goroutine, so it never starts after shutdown began.
func runUntilStopped(
	ctx context.Context, server *http.Server, listener net.Listener, endpointRunner *webhook.RequestReceivedEndpointRunner,
	worker *dex.Worker, healthCheck func(context.Context) (dex.HealthInfo, error), logger *slog.Logger,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan error, 3)
	go func() { results <- serveWebhooks(server, listener) }()
	go func() { results <- endpointRunner.Run(runCtx) }()
	dexReady := make(chan error, 1)
	go func() { dexReady <- waitForDexServer(runCtx, healthCheck, logger) }()
	pendingResults, isDexWaitPending := 2, true
	var runErr error
	for isRunning := true; isRunning; {
		select {
		case <-ctx.Done():
			isRunning = false
		case runErr = <-results:
			pendingResults--
			isRunning = false
		case err := <-dexReady:
			isDexWaitPending = false
			if err != nil {
				runErr, isRunning = err, false
				break
			}
			go func() { results <- worker.Start() }()
			pendingResults++
		}
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	stopErr := errors.Join(server.Shutdown(shutdownCtx), stopWorker(worker))
	for ; pendingResults > 0; pendingResults-- {
		<-results // Every task ends after cancellation; only the first failure is reported.
	}
	if isDexWaitPending {
		<-dexReady
	}
	if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
		// Control-C during startup replay or the Dex wait is a clean shutdown.
		runErr = nil
	}
	return errors.Join(runErr, stopErr)
}

func serveWebhooks(server *http.Server, listener net.Listener) error {
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// waitForDexServer retries the health check with delays from 250 milliseconds to 30 seconds.
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
