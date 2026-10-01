//go:build integration || live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reportarchive

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/amazon/s3"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// exampleConnection is the connection reference the example's static ConnectionName resolves to.
var exampleConnection = sdkgo.ConnectionRef{Provider: "amazon-s3", Name: ConnectionName}

type reportArchiveRun struct {
	status  dex.FlowStatus
	outcome Outcome
}

func runReportArchive(t *testing.T, ctx context.Context, client *dex.Client, flow *Flow, flowID string, input Input) reportArchiveRun {
	t.Helper()
	_, err := client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	run := reportArchiveRun{status: result.Status}
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

type reportArchiveHarness struct {
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
	cache        *blobcache.Cache
}

// newReportArchiveHarness runs the example Flow on a Worker of the Dex Server at DEX_FLOW_SERVICE_ADDRESS.
func newReportArchiveHarness(t *testing.T, providerClient *s3.Client) (*Flow, *reportArchiveHarness) {
	t.Helper()
	connection, err := s3.NewConnection(providerClient, exampleConnection)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")
	workerAddress := net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness := &reportArchiveHarness{cache: cache, workerResult: make(chan error, 1)}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(stopCtx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	return flow, harness
}

func availableIntegrationPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
