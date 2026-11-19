// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	topiclisting "github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go/examples/topic-listing/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, slog.Default()); err != nil {
		slog.Error("topiclisting stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := sns.NewProjectConnection(project, topiclisting.ConnectionName)
	if err != nil {
		return err
	}
	notificationFlow := topiclisting.NewNotificationFlow()
	registry, err := dex.NewRegistry([]dex.Flow{topiclisting.NewFlow(connection), topiclisting.NewPublishFlow(connection), topiclisting.NewSubscriptionListingFlow(connection), topiclisting.NewTopicAttributesFlow(connection), notificationFlow})
	if err != nil {
		return fmt.Errorf("register Amazon SNS topic listing Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-sns-topiclisting-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	if err != nil {
		return errors.Join(err, cache.Close())
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS"),
	})
	if err != nil {
		return errors.Join(err, stopWorker(worker), cache.Close())
	}
	// Confirm the Dex Server is reachable before starting the pull Worker.
	if err := waitForDexServer(ctx, client.HealthCheck, logger); err != nil {
		if ctx.Err() != nil {
			// Control-C while waiting is a clean shutdown, not a failure.
			err = nil
		}
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	logger.Info("Amazon SNS topic listing worker starting", "connection", topiclisting.ConnectionName)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	if err := worker.WaitPullRegistered(ctx); err != nil {
		return errors.Join(err, stopWorker(worker), <-workerResult, client.Close(), cache.Close())
	}
	if topicARN := os.Getenv("SNS_TOPIC_ARN"); topicARN != "" {
		err = runNotificationEndpoint(ctx, connection, client, worker, notificationFlow, topicARN, workerResult, logger)
		return errors.Join(err, client.Close(), cache.Close())
	}
	select {
	case <-ctx.Done():
	case err = <-workerResult:
	}
	return errors.Join(err, stopWorker(worker), client.Close(), cache.Close())
}

func runNotificationEndpoint(ctx context.Context, connection sns.Connection, client *dex.Client, worker *dex.Worker, flow *topiclisting.NotificationFlow, topicARN string, workerResult <-chan error, logger *slog.Logger) error {
	handler, err := connection.NotificationReceivedWebhookHandler()
	if err != nil {
		return errors.Join(err, stopWorker(worker), <-workerResult)
	}
	listener, err := net.Listen("tcp", environmentOr("SNS_WEBHOOK_BIND_ADDRESS", "127.0.0.1:9132"))
	if err != nil {
		return errors.Join(err, stopWorker(worker), <-workerResult)
	}
	target := sdkgo.NewDexFlowTriggerTarget(client, flow, topiclisting.AcceptEveryNotification, topiclisting.ResolveNotificationFlowID, topiclisting.MapNotificationToFlowInput)
	trigger := sns.NewNotificationReceivedTrigger(sns.NotificationReceivedTriggerConfig{
		Connection: connection, ConnectionName: topiclisting.ConnectionName, BindingName: topiclisting.NotificationBindingName,
		Configuration: sns.NotificationReceivedTriggerConfiguration{TopicARN: topicARN}, Target: target,
	})
	mux := http.NewServeMux()
	mux.Handle("/sns/notifications", handler)
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	triggerCtx, cancelTrigger := context.WithCancel(ctx)
	defer cancelTrigger()
	triggerResult := make(chan error, 1)
	serverResult := make(chan error, 1)
	go func() { triggerResult <- trigger.Run(triggerCtx) }()
	go func() { serverResult <- server.Serve(listener) }()
	logger.Info("SNS notification endpoint listening", "address", listener.Addr().String(), "path", "/sns/notifications")
	var runErr error
	workerFinished := false
	select {
	case <-ctx.Done():
	case runErr = <-workerResult:
		workerFinished = true
	case runErr = <-triggerResult:
	case runErr = <-serverResult:
	}
	cancelTrigger()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	shutdownErr := server.Shutdown(shutdownCtx)
	if errors.Is(shutdownErr, http.ErrServerClosed) {
		shutdownErr = nil
	}
	if errors.Is(runErr, context.Canceled) || errors.Is(runErr, http.ErrServerClosed) {
		runErr = nil
	}
	stopErr := stopWorker(worker)
	if !workerFinished {
		stopErr = errors.Join(stopErr, <-workerResult)
	}
	return errors.Join(runErr, shutdownErr, stopErr)
}

// waitForDexServer retries failed health checks with a WARN log, backing off from 250 ms to 30 s.
// It returns nil once the Dex Server answers, or ctx.Err() when ctx ends first.
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
