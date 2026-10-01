// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command answer-duplicate-conversation serves the Intercom connector's webhook endpoint and runs the
// Worker that answers and closes duplicate inbound conversations.
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

	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	answerduplicate "github.com/superdurable/dex-connectors-library/connectors/intercom/examples/answer-duplicate-conversation/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// webhookPath is the path to enter in the Intercom app's webhook URL behind an HTTPS tunnel.
	webhookPath   = "/webhooks/intercom"
	readinessPath = "/readyz"

	// localAPIBaseURLEnvironmentVariable points the example at a local Intercom-compatible fake for verification only.
	localAPIBaseURLEnvironmentVariable = "INTERCOM_LOCAL_API_BASE_URL"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the webhook endpoint, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("answer-duplicate-conversation stopped", "error", err)
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

// run serves the webhook endpoint at once, so notifications are recorded even while Dex is unreachable.
func run(ctx context.Context, logger *slog.Logger, connectionOptions ...intercom.Option) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	options := append(append(slices.Clone(connectionOptions), localAPIOptions(logger)...), intercom.WithLogger(logger))
	connection, err := intercom.NewLocalConnection(store, answerduplicate.ConnectionName, options...)
	if err != nil {
		return err
	}
	replyConfiguration, err := loadReplyConfiguration(store, logger)
	if err != nil {
		return err
	}
	flow := answerduplicate.NewFlow(connection, replyConfiguration)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Intercom duplicate-conversation Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-intercom-duplicate-conversation-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8851"),
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
	endpointRunner, err := newInboundEndpointRunner(store, client, flow, logger, options)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	listener, err := net.Listen("tcp", environmentOr("INTERCOM_WEBHOOK_BIND_ADDRESS", "127.0.0.1:8852"))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("Intercom webhook listening", "address", listener.Addr().String(), "path", webhookPath)
	server := &http.Server{Handler: newWebhookMux(endpointRunner), ReadHeaderTimeout: 5 * time.Second}
	runErr := runUntilStopped(ctx, server, listener, endpointRunner, worker, client.HealthCheck, logger)
	return errors.Join(runErr, client.Close(), cache.Close())
}

// loadReplyConfiguration treats an unsaved adminPicker as an empty admin, which fails each Flow with guidance.
func loadReplyConfiguration(store *localconfig.Store, logger *slog.Logger) (sdkgo.ConnectorLoadedConfiguration[answerduplicate.ReplyConfiguration], error) {
	reference := answerduplicate.ReplyConfigurationRef()
	loaded, err := localconfig.LoadOperationConfiguration[answerduplicate.ReplyConfiguration](store, reference)
	if errors.Is(err, localconfig.ErrConfigurationNotFound) {
		logger.Warn("the replying admin is not configured; choose it in Dex Web and restart", "step", reference.StepType)
		return sdkgo.ConnectorLoadedConfiguration[answerduplicate.ReplyConfiguration]{Reference: reference}, nil
	}
	return loaded, err
}

// newInboundEndpointRunner starts one Flow per new conversation, with the notification ID as request ID.
func newInboundEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *answerduplicate.Flow, logger *slog.Logger, connectionOptions []intercom.Option,
) (*intercom.ConversationEventEndpointRunner, error) {
	bindingLogger := logger.With("connector", intercom.ConnectorID, "connection", answerduplicate.ConnectionName,
		"trigger", intercom.ConversationEventTriggerDefinition.Trigger.TriggerName, "binding", answerduplicate.InboundTriggerBinding)
	return intercom.NewLocalConversationEventEndpointRunner(store, answerduplicate.ConnectionName, []intercom.LocalConversationEventTriggerRoute{{
		BindingName: answerduplicate.InboundTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, answerduplicate.AcceptInboundConversation, answerduplicate.ResolveFlowID,
			answerduplicate.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, connectionOptions...)
}

// localAPIOptions redirects API calls only when the local-verification variable is set.
func localAPIOptions(logger *slog.Logger) []intercom.Option {
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		logger.Warn("Intercom API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		return []intercom.Option{intercom.WithAPIBaseURL(baseURL)}
	}
	return nil
}

// newWebhookMux mounts the endpoint and a readiness check that passes once the binding receives events.
func newWebhookMux(endpointRunner *intercom.ConversationEventEndpointRunner) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(webhookPath, endpointRunner)
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
	ctx context.Context, server *http.Server, listener net.Listener, endpointRunner *intercom.ConversationEventEndpointRunner,
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
