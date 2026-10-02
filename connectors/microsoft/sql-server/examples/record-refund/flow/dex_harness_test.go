//go:build integration || live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordrefund

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// testWorker runs the example Flow on a real Worker against DEX_FLOW_SERVICE_ADDRESS.
type testWorker struct {
	flow   *Flow
	client *dex.Client
	worker *dex.Worker
	result chan error
}

func startTestWorker(t *testing.T, config sqlserver.Config, password string) *testWorker {
	t.Helper()
	client, err := sqlserver.New(config, sdkgo.StaticCredentialProvider[sqlserver.Credentials]{
		{Provider: "microsoft-sql-server", Name: ConnectionName}: {Password: sdkgo.NewSecretString(password)},
	})
	require.NoError(t, err)
	connection, err := sqlserver.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft-sql-server", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	flowServiceAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: address, FlowServiceAddress: flowServiceAddress})
	require.NoError(t, err)
	dexClient, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: flowServiceAddress, WorkerTarget: &dex.WorkerTarget{Address: address}})
	require.NoError(t, err)
	harness := &testWorker{flow: flow, client: dexClient, worker: worker, result: make(chan error, 1)}
	go func() { harness.result <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, worker.Stop(ctx))
		require.NoError(t, <-harness.result)
		require.NoError(t, errors.Join(dexClient.Close(), cache.Close()))
	})
	return harness
}

// runRefundFlow starts one uniquely identified Flow and waits for its terminal result.
func (harness *testWorker) runRefundFlow(t *testing.T, input Input) (string, dex.FlowResult) {
	t.Helper()
	flowID := fmt.Sprintf("sql-server-record-refund-%s-%d", input.OrderID, time.Now().UnixNano())
	requestID := "start-" + flowID
	_, err := harness.client.StartFlow(context.Background(), harness.flow, flowID, input, dex.StartFlowOptions{RequestID: &requestID})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	for {
		result, err := harness.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		return flowID, result
	}
}

func (harness *testWorker) requireCompletedOutcome(t *testing.T, flowID string, result dex.FlowResult) Outcome {
	t.Helper()
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	var display map[string]any
	require.NoError(t, harness.client.InvokeRPC(context.Background(), flowID, harness.flow.GetDexDisplay, nil, &display))
	encoded, err := json.Marshal(display["sql-server-refund-outcome"])
	require.NoError(t, err)
	var displayed Outcome
	require.NoError(t, json.Unmarshal(encoded, &displayed))
	require.Equal(t, outcome, displayed, "the outcome Attribute matches the Flow result")
	return outcome
}
