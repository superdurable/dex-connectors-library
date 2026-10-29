//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/square"
	"github.com/superdurable/dex-connectors-library/connectors/square/squaremock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestResourceReadUsesManifestMockWithRealDex(t *testing.T) {
	mock := squaremock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	workerOptions := dex.WorkerOptions{FlowServiceAddress: serverAddress}
	setLegacyWorkerAddress(&workerOptions, workerAddress)
	worker, err := dex.NewWorker(registry, cache, workerOptions)
	require.NoError(t, err)
	clientOptions := dex.ClientOptions{FlowServiceAddress: serverAddress}
	setLegacyClientWorkerTarget(&clientOptions, workerAddress)
	client, err := dex.NewClient(registry, cache, clientOptions)
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "square-resource-read-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Request{Cursor: ""}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.NotEmpty(t, outcome.Value.Payments)
	calls := mock.ListPayments().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, square.ListPaymentsInput{Cursor: ""}, calls[0].Input)
	require.Equal(t, square.ListPaymentsBranchListed, calls[0].Branch)
}

// setLegacyWorkerAddress configures the listening Worker fields removed by pull-only Dex SDKs.
func setLegacyWorkerAddress(options *dex.WorkerOptions, address string) {
	fields := reflect.ValueOf(options).Elem()
	if bindAddress := fields.FieldByName("BindAddress"); bindAddress.IsValid() {
		bindAddress.SetString(address)
	}
	if workerTarget := fields.FieldByName("WorkerTarget"); workerTarget.IsValid() {
		workerTarget.FieldByName("Address").SetString(address)
	}
}

// setLegacyClientWorkerTarget routes old SDK Flows to the listening Worker.
func setLegacyClientWorkerTarget(options *dex.ClientOptions, address string) {
	workerTarget := reflect.ValueOf(options).Elem().FieldByName("WorkerTarget")
	if !workerTarget.IsValid() {
		return
	}
	target := reflect.New(workerTarget.Type().Elem())
	target.Elem().FieldByName("Address").SetString(address)
	workerTarget.Set(target)
}
