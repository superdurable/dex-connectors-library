//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package blobdownload

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/azure-blob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestCreateContainerRecoversAcrossDexRetry(t *testing.T) {
	var putCount atomic.Int32
	var headCount atomic.Int32
	var createCallID atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/docs", request.URL.Path)
		require.Equal(t, "container", request.URL.Query().Get("restype"))
		switch request.Method {
		case http.MethodPut:
			callID := request.Header.Get("x-ms-meta-dex_create_call_id")
			require.NotEmpty(t, callID)
			if putCount.Add(1) == 1 {
				createCallID.Store(callID)
				writer.WriteHeader(http.StatusCreated)
				connection, _, err := writer.(http.Hijacker).Hijack()
				require.NoError(t, err)
				require.NoError(t, connection.Close())
				return
			}
			require.Equal(t, createCallID.Load(), callID)
			writer.Header().Set("x-ms-error-code", "ContainerAlreadyExists")
			writer.WriteHeader(http.StatusConflict)
		case http.MethodHead:
			headCount.Add(1)
			writer.Header().Set("x-ms-meta-dex_create_call_id", createCallID.Load().(string))
			writer.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected method %s", request.Method)
		}
	}))
	defer server.Close()
	reference := sdkgo.ConnectionRef{Provider: "microsoft-azure-blob", Name: ConnectionName}
	credentials := azureblob.Credentials{AccountKey: sdkgo.NewSecretString("c2VjcmV0LXRlc3Qta2V5")}
	provider, err := azureblob.New(azureblob.Config{Endpoint: server.URL, AccountName: "exampleacct"}, sdkgo.StaticCredentialProvider[azureblob.Credentials]{reference: credentials}, azureblob.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	connection, err := azureblob.NewConnection(provider, reference)
	require.NoError(t, err)
	flow := NewContainerCreationFlow(connection)
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
	flowID := "azureblob-container-create-retry-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, azureblob.CreateContainerInput{Container: "docs"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output azureblob.ContainerWriteResult
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "docs", output.Container)
	require.Equal(t, int32(2), putCount.Load())
	require.Equal(t, int32(1), headCount.Load())
}
