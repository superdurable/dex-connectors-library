// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command issue-events serves the Linear connector's webhook endpoint and runs the Worker that records each
// newly created issue and reads it back.
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

	"github.com/superdurable/dex-connectors-library/connectors/linear"
	issueevents "github.com/superdurable/dex-connectors-library/connectors/linear/examples/issue-events/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// webhookPath is the Linear webhook's URL path; expose it through an HTTPS tunnel in local development.
	webhookPath   = "/webhooks/linear"
	readinessPath = "/readyz"
	// localAPIURLEnvironmentVariable points getIssue at a local Linear-compatible fake for verification only.
	localAPIURLEnvironmentVariable = "LINEAR_LOCAL_API_URL"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the webhook endpoint, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger, connectionOptions()...); err != nil {
		logger.Error("issue-events stopped", "error", err)
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

// connectionOptions redirects API calls only when the local-verification variable is set.
func connectionOptions() []linear.Option {
	if apiURL := os.Getenv(localAPIURLEnvironmentVariable); apiURL != "" {
		slog.Warn("Linear API requests go to a local URL", "variable", localAPIURLEnvironmentVariable)
		return []linear.Option{linear.WithAPIURL(apiURL)}
	}
	return nil
}

// run serves the webhook endpoint at once, so deliveries are recorded even while Dex is unreachable.
func run(ctx context.Context, logger *slog.Logger, connectionOptions ...linear.Option) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := linear.NewProjectConnection(project, issueevents.ConnectionName, connectionOptions...)
	if err != nil {
		return err
	}
	flow := issueevents.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Linear issue event Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-linear-issue-events-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8837"),
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
	endpointRunner, err := newIssueEndpointRunner(project, client, flow, logger, connectionOptions)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	listener, err := net.Listen("tcp", environmentOr("WEBHOOK_BIND_ADDRESS", "127.0.0.1:8838"))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("issue-events webhook listening", "address", listener.Addr().String(), "path", webhookPath)
	server := &http.Server{Handler: newWebhookMux(endpointRunner), ReadHeaderTimeout: 5 * time.Second}
	runErr := runUntilStopped(ctx, server, listener, endpointRunner, worker, client.HealthCheck, logger)
	return errors.Join(runErr, client.Close(), cache.Close())
}

// newIssueEndpointRunner serves the issue-created binding from the project configuration through its
// durable project inbox.
func newIssueEndpointRunner(
	project *projectconfig.LoadedProject, client *dex.Client, flow *issueevents.Flow, logger *slog.Logger, connectionOptions []linear.Option,
) (*linear.IssueEventReceivedEndpointRunner, error) {
	return linear.NewProjectIssueEventReceivedEndpointRunner(project, issueevents.ConnectionName, []linear.ProjectIssueEventReceivedTriggerRoute{{
		BindingName: issueevents.IssueCreatedTriggerBinding, Target: newIssueTarget(client, flow, logger),
	}}, append(slices.Clone(connectionOptions), linear.WithLogger(logger))...)
}

// newIssueTarget starts one Flow per created issue, with the Trigger event ID as request ID.
func newIssueTarget(client *dex.Client, flow *issueevents.Flow, logger *slog.Logger) sdkgo.TriggerTarget[linear.IssueEvent] {
	bindingLogger := logger.With("connector", linear.ConnectorID, "connection", issueevents.ConnectionName,
		"trigger", linear.IssueEventReceivedTriggerDefinition.Trigger.TriggerName, "binding", issueevents.IssueCreatedTriggerBinding)
	return sdkgo.NewDexFlowTriggerTarget(client, flow, issueevents.AcceptIssueCreated, issueevents.ResolveFlowID,
		issueevents.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger))
}

// newWebhookMux mounts the endpoint and a readiness check that passes once the binding receives events.
func newWebhookMux(endpointRunner *linear.IssueEventReceivedEndpointRunner) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	mux.HandleFunc(readinessPath, func(response http.ResponseWriter, _ *http.Request) {
		if endpointRunner.RunningSourceCount() == 0 {
			http.Error(response, "Linear binding is not receiving yet", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(response, "ready\n") // The status already answered the check.
	})
	return mux
}

// runUntilStopped starts the Worker only from this goroutine, so it never starts after shutdown began.
func runUntilStopped(
	ctx context.Context, server *http.Server, listener net.Listener, endpointRunner *linear.IssueEventReceivedEndpointRunner,
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
	stopErr := errors.Join(shutdownServer(shutdownCtx, server), stopWorker(worker))
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

// shutdownServer closes connections still open at the deadline, such as one that never sent a request.
func shutdownServer(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return server.Close()
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
