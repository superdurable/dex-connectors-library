// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/slack"
	threadapproval "github.com/superdurable/dex-connectors-library/connectors/slack/examples/thread-approval/flow"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	store, err := localconfig.LoadFromEnvironment()
	if err != nil {
		return err
	}
	connection, err := slack.NewLocalConnection(store, threadapproval.ConnectionName)
	if err != nil {
		return err
	}
	flow := threadapproval.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	if err != nil {
		return fmt.Errorf("register Slack thread approval Flow: %w", err)
	}
	cache, err := blobcache.New(&blobcache.Config{
		Dir: environmentOr("DEX_BLOB_CACHE_DIR", filepath.Join(os.TempDir(), "dex-slack-thread-approval-blobs")), MaxBytes: 1 << 30,
	})
	if err != nil {
		return fmt.Errorf("create blob cache: %w", err)
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress:        environmentOr("DEX_WORKER_BIND_ADDRESS", "127.0.0.1:8813"),
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
	var startTriggerConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	if err := store.DecodeTriggerConfiguration(
		slack.ConnectorID, threadapproval.ConnectionName, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName,
		threadapproval.StartTriggerBinding, &startTriggerConfiguration,
	); err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	startEventFilter, err := threadapproval.NewStartTriggerEventFilter(startTriggerConfiguration)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	startRunner, err := slack.NewLocalChannelThreadCreatedTrigger(
		store, threadapproval.ConnectionName, threadapproval.StartTriggerBinding,
		sdkgo.NewDexFlowTriggerTarget(
			client, flow, startEventFilter, slack.FlowIDByThread(threadapproval.ResolveFlowID), threadapproval.BuildStartInput,
		),
	)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	var replyTriggerConfiguration slack.ThreadReplyCreatedTriggerConfiguration
	if err := store.DecodeTriggerConfiguration(
		slack.ConnectorID, threadapproval.ConnectionName, slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName,
		threadapproval.ReplyTriggerBinding, &replyTriggerConfiguration,
	); err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	replyEventFilter, err := threadapproval.NewReplyTriggerEventFilter(replyTriggerConfiguration)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	replyRunner, err := slack.NewLocalThreadReplyCreatedTrigger(
		store, threadapproval.ConnectionName, threadapproval.ReplyTriggerBinding,
		sdkgo.NewDexRPCTriggerTarget(
			client, flow.ReceiveThreadReply, replyEventFilter, slack.FlowIDByThread(threadapproval.ResolveFlowID),
		),
	)
	if err != nil {
		return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runResults := make(chan error, 3)
	go func() { runResults <- worker.Start() }()
	go func() { runResults <- startRunner.Run(runCtx) }()
	go func() { runResults <- replyRunner.Run(runCtx) }()
	select {
	case <-ctx.Done():
	case err = <-runResults:
	}
	cancel()
	return errors.Join(err, client.Close(), stopWorker(worker), cache.Close())
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
