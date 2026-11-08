//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventcapture

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go/posthogmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestEventCaptureUsesMockWithRealDex(t *testing.T) {
	mock := posthogmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
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
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "posthog-event-capture-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Request{Event: "Signed Up", DistinctID: "member-1"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	calls := mock.CaptureEvent().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	require.Equal(t, "member-1", calls[0].Input.DistinctID)
}

func TestEventCaptureRetriesServiceFailureWithRealDex(t *testing.T) {
	var requests atomic.Int32
	var identityMu sync.Mutex
	var firstUUID, firstTimestamp string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			UUID      string `json:"uuid"`
			Timestamp string `json:"timestamp"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		identityMu.Lock()
		if firstUUID == "" {
			firstUUID, firstTimestamp = payload.UUID, payload.Timestamp
		} else {
			require.Equal(t, firstUUID, payload.UUID)
			require.Equal(t, firstTimestamp, payload.Timestamp)
		}
		identityMu.Unlock()
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, err := w.Write([]byte(`{"status":"Ok"}`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	reference := sdkgo.ConnectionRef{Provider: "posthog", Name: ConnectionName}
	providerClient, err := posthog.New(posthog.Config{CaptureURL: provider.URL, AppURL: provider.URL}, sdkgo.StaticCredentialProvider[posthog.Credentials]{
		reference: {ProjectToken: sdkgo.NewSecretString("phc_test-token")},
	})
	require.NoError(t, err)
	connection, err := posthog.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
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
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "posthog-event-capture-retry-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Request{Event: "Signed Up", DistinctID: "member-1", Timestamp: "2026-10-29T00:00:00Z"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	require.EqualValues(t, 2, requests.Load())
	var output posthog.CapturedEvent
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.True(t, output.Accepted)
	require.Equal(t, firstUUID, output.EventID)
}
