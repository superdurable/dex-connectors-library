//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go/basecampmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPersonEnrollmentUsesDefaultMockWithRealDex(t *testing.T) {
	mock := basecampmock.New(t, ConnectionName)
	flow := NewPersonEnrollmentFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "basecamp-person-enrollment-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, basecamp.EnrollPersonInput{AccountID: "123", Name: "Example Person", EmailAddress: "person@example.com"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output basecamp.EnrollmentResult
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "789", output.Person.ID)
	require.Equal(t, "Example Person", output.Person.Name)
	require.Len(t, mock.EnrollPerson().Calls(), 1)
}
