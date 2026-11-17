//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactionread

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/braintree/go"
	"github.com/superdurable/dex-connectors-library/connectors/braintree/go/braintreemock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDisputeFileUploadUsesDefaultMockWithRealDex(t *testing.T) {
	mock := braintreemock.New(t, ConnectionName)
	flow := NewDisputeFileUploadFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "braintree-dispute-file-upload-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, braintree.CreateDisputeFileEvidenceInput{DisputeID: "dispute-1", Category: "GENERAL", FileName: "sample.pdf", ExpectedSHA256: "a580a4a92ef8d5c28ba07d43410bd8bdd0ea470461593dc7e5fbb354174098b9"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output braintree.DisputeEvidenceProfile
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "evidence-file-1", output.ID)
	require.Equal(t, "GENERAL", output.Category)
	require.Len(t, mock.CreateDisputeFileEvidence().Calls(), 1)
}
