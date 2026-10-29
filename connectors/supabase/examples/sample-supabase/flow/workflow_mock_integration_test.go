//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/supabase"
	"github.com/superdurable/dex-connectors-library/connectors/supabase/supabasemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestMockDefaultCompletesWithRealDex(t *testing.T) {
	mock := supabasemock.New(t, ConnectionName)
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
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: workerAddress, FlowServiceAddress: serverAddress, WorkerTarget: dex.WorkerTarget{Address: workerAddress}})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress}})
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
	flowID := "supabase-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := supabase.SelectRowsInput{Table: "projects", Limit: 10}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output supabase.SelectRowsOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	calls := mock.SelectRows().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, input, calls[0].Input)
}
