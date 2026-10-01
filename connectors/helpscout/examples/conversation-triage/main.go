// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command conversation-triage serves the Help Scout connector's webhook endpoint and runs the Worker that
// triages each new conversation.
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

	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// webhookPath is the Help Scout webhook's callback path; expose it through an HTTPS tunnel in local development.
	webhookPath   = "/webhooks/helpscout"
	readinessPath = "/readyz"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the webhook endpoint, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("conversation-triage stopped", "error", err)
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

// run serves the webhook endpoint at once, so deliveries are recorded even while Dex is unreachable.
func run(ctx context.Context, logger *slog.Logger, connectionOptions ...helpscout.Option) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := helpscout.NewLocalRenewingConnection(store, conversationtriage.ConnectionName, connectionOptions...)
	if err != nil {
		return err
	}
	flow := conversationtriage.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register conversation triage Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-helpscout-conversation-triage-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8871"),
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
	endpointRunner, err := newConversationEndpointRunner(store, client, flow, logger, connectionOptions)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	listener, err := net.Listen("tcp", environmentOr("WEBHOOK_BIND_ADDRESS", "127.0.0.1:8872"))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("conversation-triage webhook listening", "address", listener.Addr().String(), "path", webhookPath)
	server := &http.Server{Handler: newWebhookMux(endpointRunner), ReadHeaderTimeout: 5 * time.Second}
	runErr := runUntilStopped(ctx, server, listener, endpointRunner, worker, client.HealthCheck, logger)
	return errors.Join(runErr, client.Close(), cache.Close())
}

// newConversationEndpointRunner starts one Flow per new conversation, with the Trigger event ID as request ID.
func newConversationEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *conversationtriage.Flow, logger *slog.Logger, connectionOptions []helpscout.Option,
) (*helpscout.ConversationEventEndpointRunner, error) {
	bindingLogger := logger.With("connector", helpscout.ConnectorID, "connection", conversationtriage.ConnectionName,
		"trigger", helpscout.ConversationEventTriggerDefinition.Trigger.TriggerName, "binding", conversationtriage.NewConversationTriggerBinding)
	return helpscout.NewLocalConversationEventEndpointRunner(store, conversationtriage.ConnectionName, []helpscout.LocalConversationEventTriggerRoute{{
		BindingName: conversationtriage.NewConversationTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, conversationtriage.AcceptNewConversation, conversationtriage.ResolveFlowID,
			conversationtriage.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, append(slices.Clone(connectionOptions), helpscout.WithLogger(logger))...)
}

// newWebhookMux mounts the endpoint and a readiness check that passes once the binding receives events.
func newWebhookMux(endpointRunner *helpscout.ConversationEventEndpointRunner) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
	mux.HandleFunc(readinessPath, func(response http.ResponseWriter, _ *http.Request) {
		if endpointRunner.RunningSourceCount() == 0 {
			http.Error(response, "Help Scout binding is not receiving yet", http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(response, "ready\n") // The status already answered the check.
	})
	return mux
}

// runUntilStopped starts the Worker only from this goroutine, so it never starts after shutdown began.
func runUntilStopped(
	ctx context.Context, server *http.Server, listener net.Listener, endpointRunner *helpscout.ConversationEventEndpointRunner,
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
