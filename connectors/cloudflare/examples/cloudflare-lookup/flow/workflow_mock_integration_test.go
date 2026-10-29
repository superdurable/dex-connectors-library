//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package cloudflarelookup

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
	"github.com/superdurable/dex-connectors-library/connectors/cloudflare"
	"github.com/superdurable/dex-connectors-library/connectors/cloudflare/cloudflaremock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestLookupUsesMockDefaultWithRealDex(t *testing.T) {
	mock := cloudflaremock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
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
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(client.Close(), worker.Stop(stopContext), <-workerResult))
	}()
	contextWithDeadline, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := cloudflare.ListZonesInput{}
	flowID := "cloudflare-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(contextWithDeadline, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(contextWithDeadline, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output cloudflare.ZonesPage
	require.NoError(t, result.DecodeSingleOutput(&output))
	calls := mock.ListZones().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, request, calls[0].Input)
	require.Equal(t, cloudflare.ListZonesBranchListed, calls[0].Branch)
	require.Equal(t, flowID, calls[0].FlowID)
}
