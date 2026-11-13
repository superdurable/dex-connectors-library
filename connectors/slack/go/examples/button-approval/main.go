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

	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	buttonapproval "github.com/superdurable/dex-connectors-library/connectors/slack/go/examples/button-approval/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
		logger.Error("button-approval stopped", "error", err)
		os.Exit(1)
	}
}

// newLogger writes text records to output at LOG_LEVEL (debug, info, warn, or error; info by default).
// Set LOG_LEVEL=debug to see every Slack message and click the Triggers ignore.
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
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := slack.NewProjectConnection(project, buttonapproval.ConnectionName)
	if err != nil {
		return err
	}
	flow := buttonapproval.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Slack button approval Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-slack-button-approval-blobs")), MaxBytes: 1 << 30,
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
	triggerRunner, err := newTriggerRunner(project, client, flow, logger)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	err = runWorkerAndTriggers(ctx, worker, triggerRunner, client.HealthCheck, logger)
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
}

// newTriggerRunner runs the request and button bindings on one shared Socket Mode connection, because Slack
// sends each event to only one of the app's connections.
func newTriggerRunner(project *projectconfig.LoadedProject, client *dex.Client, flow *buttonapproval.ButtonApprovalFlow, logger *slog.Logger) (*slack.MessageTriggerRunner, error) {
	var startConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	if err := project.Configuration.DecodeTriggerConfiguration(
		slack.ConnectorID, buttonapproval.ConnectionName, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName,
		buttonapproval.StartTriggerBinding, &startConfiguration,
	); err != nil {
		return nil, err
	}
	startFilter, err := buttonapproval.NewStartTriggerFilter(startConfiguration)
	if err != nil {
		return nil, err
	}
	var buttonConfiguration slack.ButtonClickedTriggerConfiguration
	if err := project.Configuration.DecodeTriggerConfiguration(
		slack.ConnectorID, buttonapproval.ConnectionName, slack.ButtonClickedTriggerDefinition.Trigger.TriggerName,
		buttonapproval.ButtonTriggerBinding, &buttonConfiguration,
	); err != nil {
		return nil, err
	}
	buttonFilter, err := buttonapproval.NewButtonTriggerFilter(buttonConfiguration)
	if err != nil {
		return nil, err
	}
	return slack.NewProjectMessageTriggerRunner(project, buttonapproval.ConnectionName, slack.ProjectMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []slack.ProjectChannelThreadCreatedTriggerRoute{{
			BindingName: buttonapproval.StartTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(
				client, flow, startFilter, buttonapproval.ResolveFlowID, buttonapproval.MapToFlowInput,
				sdkgo.WithTriggerLogger(bindingLogger(logger, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, buttonapproval.StartTriggerBinding)),
			),
		}},
		ButtonClickedRoutes: []slack.ProjectButtonClickedTriggerRoute{{
			BindingName: buttonapproval.ButtonTriggerBinding,
			Target: sdkgo.NewDexRPCTriggerTarget(
				client, flow.ReceiveButtonDecision, buttonFilter, buttonapproval.ResolveButtonFlowID, buttonapproval.MapToReceiveButtonDecisionInput,
				sdkgo.WithTriggerLogger(bindingLogger(logger, slack.ButtonClickedTriggerDefinition.Trigger.TriggerName, buttonapproval.ButtonTriggerBinding)),
			),
		}},
	}, slack.WithLogger(logger))
}

// runWorkerAndTriggers waits for the Dex Server, then runs the Worker and the Trigger runner until ctx ends or
// either one fails.
func runWorkerAndTriggers(
	ctx context.Context, worker *dex.Worker, triggerRunner *slack.MessageTriggerRunner,
	healthCheck func(context.Context) (dex.HealthInfo, error), logger *slog.Logger,
) error {
	if err := waitForDexServer(ctx, healthCheck, logger); err != nil {
		if ctx.Err() != nil {
			// Control-C while waiting is a clean shutdown, not a failure.
			err = nil
		}
		return err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runResults := make(chan error, 2)
	go func() { runResults <- worker.Start() }()
	go func() { runResults <- triggerRunner.Run(runCtx) }()
	var err error
	select {
	case <-ctx.Done():
	case err = <-runResults:
	}
	cancel()
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		// Control-C during a long startup replay is a clean shutdown, not a failure.
		err = nil
	}
	return err
}

// bindingLogger labels a Dex target's records with the connector, connection, trigger, and binding keys that the
// runner and the durable inbox use.
func bindingLogger(logger *slog.Logger, trigger string, binding string) *slog.Logger {
	return logger.With("connector", slack.ConnectorID, "connection", buttonapproval.ConnectionName, "trigger", trigger, "binding", binding)
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
