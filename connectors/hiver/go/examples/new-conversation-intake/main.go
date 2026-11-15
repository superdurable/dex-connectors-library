// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Command new-conversation-intake polls the picked Hiver shared inbox with the conversationReceived Trigger
// and runs the Worker that records each new conversation in its own Flow.
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

	hiver "github.com/superdurable/dex-connectors-library/connectors/hiver/go"
	newconversationintake "github.com/superdurable/dex-connectors-library/connectors/hiver/go/examples/new-conversation-intake/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// localAPIBaseURLEnvironmentVariable points the connection at a local Hiver stand-in for verification.
const localAPIBaseURLEnvironmentVariable = "HIVER_LOCAL_API_BASE_URL"

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK, the poll runner, and the durable inbox all log to slog.Default().
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("new-conversation-intake stopped", "error", err)
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
	connection, err := hiver.NewProjectConnection(project, newconversationintake.ConnectionName, options...)
	if err != nil {
		return err
	}
	flow := newconversationintake.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Hiver new-conversation intake Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-hiver-new-conversation-intake-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	// One durable runner records the baseline, records each new conversation in the binding's project inbox,
	// and starts one Flow per conversation; it logs every failed poll and delivery.
	triggerRunner, err := hiver.NewProjectConversationReceivedTriggerRunner(project, newconversationintake.ConnectionName,
		[]hiver.ProjectConversationReceivedTriggerRoute{{
			BindingName: newconversationintake.ConversationReceivedTriggerBinding, Target: newConversationTarget(client, flow, logger),
		}}, options...)
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

// connectionOptions logs through logger and redirects API calls only when the local-verification variable is set.
func connectionOptions(logger *slog.Logger) []hiver.Option {
	options := []hiver.Option{hiver.WithLogger(logger)}
	if baseURL := os.Getenv(localAPIBaseURLEnvironmentVariable); baseURL != "" {
		logger.Warn("Hiver API requests go to a local base URL", "variable", localAPIBaseURLEnvironmentVariable)
		options = append(options, hiver.WithAPIBaseURL(baseURL))
	}
	return options
}

// newConversationTarget starts one Flow per new conversation, with the Trigger event ID as the Flow ID suffix.
func newConversationTarget(client *dex.Client, flow *newconversationintake.Flow, logger *slog.Logger) sdkgo.TriggerTarget[hiver.ConversationReceivedEvent] {
	bindingLogger := logger.With("connector", hiver.ConnectorID, "connection", newconversationintake.ConnectionName,
		"trigger", hiver.ConversationReceivedTriggerDefinition.Trigger.TriggerName, "binding", newconversationintake.ConversationReceivedTriggerBinding)
	return sdkgo.NewDexFlowTriggerTarget(client, flow, newconversationintake.AcceptEveryConversation, newconversationintake.ResolveFlowID,
		newconversationintake.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger))
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
