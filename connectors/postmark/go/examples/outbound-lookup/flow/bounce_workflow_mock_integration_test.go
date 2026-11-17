//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/connectors/postmark/go/postmarkmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMockBackedBounceFlowWithRealDex(t *testing.T) {
	mock := postmarkmock.New(t, ConnectionName)
	flow := NewBounceFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: os.Getenv("DEX_FLOW_SERVICE_ADDRESS")})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
	cancelRegistration()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "postmark-bounce-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := BounceRequest{Count: 20}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	var result dex.FlowResult
	for {
		result, err = client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
		var timeout *dex.LongPollTimeoutError
		if errors.As(err, &timeout) {
			continue
		}
		require.NoError(t, err)
		break
	}
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output postmark.BouncePage
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.NotEmpty(t, output.Bounces)
	var display map[string]any
	require.NoError(t, client.InvokeRPC(ctx, flowID, flow.GetDexDisplay, nil, &display))
	require.Contains(t, display, "postmark-bounce-page")
	calls := mock.ListBounces().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, postmark.ListBouncesInput(input), calls[0].Input)
	require.Equal(t, postmark.ListBouncesBranchListed, calls[0].Branch)
}
