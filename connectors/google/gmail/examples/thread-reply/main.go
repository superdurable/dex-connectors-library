// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

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

	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	threadreply "github.com/superdurable/dex-connectors-library/connectors/google/gmail/examples/thread-reply/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	logger := newLogger(os.Stderr, os.Getenv("LOG_LEVEL"))
	// The Dex SDK and any other library that logs to slog.Default() share the same handler.
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("thread-reply stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
// Set LOG_LEVEL=debug to see every Gmail message the Triggers ignore and every delivery.
func newLogger(output io.Writer, levelName string) *slog.Logger {
	level := slog.LevelInfo
	invalid := false
	if strings.TrimSpace(levelName) != "" {
		invalid = level.UnmarshalText([]byte(strings.TrimSpace(levelName))) != nil
	}
	logger := slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
	if invalid {
		logger.Warn("LOG_LEVEL is not debug, info, warn, or error; using info", "log_level", levelName)
	}
	return logger
}

func run(ctx context.Context, logger *slog.Logger) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := gmail.NewLocalConnection(store, threadreply.ConnectionName)
	if err != nil {
		return err
	}
	replyMessageConfiguration, err := localconfig.LoadOperationConfiguration[threadreply.ReplyMessageConfiguration](
		store, threadreply.ReplyMessageConfigurationRef(),
	)
	if err != nil {
		return err
	}
	flow := threadreply.NewFlow(connection, replyMessageConfiguration)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Gmail thread reply Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-gmail-thread-reply-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8814"),
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
	var startTriggerConfiguration gmail.MessageReceivedTriggerConfiguration
	if err := store.DecodeTriggerConfiguration(
		gmail.ConnectorID, threadreply.ConnectionName, gmail.MessageReceivedTriggerDefinition.Trigger.TriggerName,
		threadreply.StartTriggerBinding, &startTriggerConfiguration,
	); err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	startTriggerFilter, err := threadreply.NewStartTriggerFilter(startTriggerConfiguration)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	var replyTriggerConfiguration gmail.ReplyReceivedTriggerConfiguration
	if err := store.DecodeTriggerConfiguration(
		gmail.ConnectorID, threadreply.ConnectionName, gmail.ReplyReceivedTriggerDefinition.Trigger.TriggerName,
		threadreply.ReplyTriggerBinding, &replyTriggerConfiguration,
	); err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	replyTriggerFilter, err := threadreply.NewReplyTriggerFilter(replyTriggerConfiguration)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	// One ordered runner delivers every root message before any reply in the same poll. The runner, its
	// durable inboxes, and the Dex targets log every skipped message and every retry, so the example needs
	// no wrapper of its own.
	triggerRunner, err := gmail.NewLocalMessageTriggerRunner(store, threadreply.ConnectionName, gmail.LocalMessageTriggerRunnerConfig{
		MessageReceivedRoutes: []gmail.LocalMessageReceivedTriggerRoute{{
			BindingName: threadreply.StartTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(
				client, flow, startTriggerFilter, threadreply.ResolveFlowID, threadreply.MapToFlowInput,
				sdkgo.WithTriggerLogger(bindingLogger(
					logger, gmail.MessageReceivedTriggerDefinition.Trigger.TriggerName, threadreply.StartTriggerBinding,
				)),
			),
		}},
		ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{
			BindingName: threadreply.ReplyTriggerBinding,
			Target: sdkgo.NewDexRPCTriggerTarget(
				client, flow.ReceiveEmailReply, replyTriggerFilter, threadreply.ResolveFlowID,
				threadreply.MapToReceiveEmailReplyInput,
				sdkgo.WithTriggerLogger(bindingLogger(
					logger, gmail.ReplyReceivedTriggerDefinition.Trigger.TriggerName, threadreply.ReplyTriggerBinding,
				)),
			),
		}},
	}, gmail.WithLogger(logger))
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	// Worker.Start fails at once when the Dex Server is unreachable, so wait for it first.
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

// bindingLogger labels a Dex target's records, such as a filtered message, with the same connector,
// connection, trigger, and binding keys that the runner and the durable inbox use.
func bindingLogger(logger *slog.Logger, trigger string, binding string) *slog.Logger {
	return logger.With("connector", gmail.ConnectorID, "connection", threadreply.ConnectionName, "trigger", trigger, "binding", binding)
}

// waitForDexServer returns once the Dex Server answers a health check. It logs a WARN record for each
// failed check and retries with delays that grow from 250 milliseconds to 30 seconds, then logs an INFO
// record when a later check succeeds. It returns ctx.Err() when ctx ends first.
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
