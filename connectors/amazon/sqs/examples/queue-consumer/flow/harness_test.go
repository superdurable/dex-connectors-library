//go:build integration || live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// exampleConnection is the connection reference the example's static ConnectionName resolves to.
var exampleConnection = sdkgo.ConnectionRef{Provider: "amazon-sqs", Name: ConnectionName}

type queueConsumerRun struct {
	status       dex.FlowStatus
	errorMessage string
	outcome      Outcome
}

func runQueueConsumer(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, input Input) queueConsumerRun {
	t.Helper()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := queueConsumerRun{status: result.Status, errorMessage: result.ErrorMessage}
	if result.Status == dex.FlowCompleted {
		require.NoError(t, result.DecodeSingleOutput(&run.outcome))
	}
	return run
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

type queueConsumerHarness struct {
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
	cache        *blobcache.Cache
}

// newQueueConsumerHarness runs the example Flow on a Worker of the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func newQueueConsumerHarness(t *testing.T, providerClient *sqs.Client) (*Flow, *queueConsumerHarness) {
	t.Helper()
	connection, err := sqs.NewConnection(providerClient, exampleConnection)
	require.NoError(t, err)
	return newQueueConsumerHarnessWithConnection(t, connection)
}

func newQueueConsumerHarnessWithConnection(t *testing.T, connection sqs.Connection) (*Flow, *queueConsumerHarness) {
	t.Helper()
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	harness := &queueConsumerHarness{cache: cache, workerResult: make(chan error, 1)}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	go func() { harness.workerResult <- harness.worker.Start() }()
	require.NoError(t, harness.worker.WaitPullRegistered(t.Context()))
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
