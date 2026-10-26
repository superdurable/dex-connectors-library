//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUpsertContactExampleRetries503ThroughRealDex(t *testing.T) {
	var mutex sync.Mutex
	var writeKeys []string
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = response.Write([]byte(`{"range":"Contacts","majorDimension":"ROWS","values":[["email","name","status"]]}`))
			return
		}
		require.Equal(t, http.MethodPost, request.Method)
		mutex.Lock()
		writeKeys = append(writeKeys, request.Header.Get("X-Idempotency-Key"))
		requestNumber := len(writeKeys)
		mutex.Unlock()
		if requestNumber == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Contacts'!A2:C2"}}`))
	}))
	defer provider.Close()
	connectionReference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	providerClient, err := spreadsheet.New(spreadsheet.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		connectionReference: {AccessToken: sdkgo.NewSecretString("test-token")},
	})
	require.NoError(t, err)
	connection, err := spreadsheet.NewConnection(providerClient, connectionReference)
	require.NoError(t, err)
	flow := NewFlow(connection)
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
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "google-sheets-upsert-contact-retry-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Input{
		SpreadsheetID: "sheet", SheetName: "Contacts", Email: "ada@example.test", Name: "Ada", Status: "active",
	}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output spreadsheet.UpsertRowOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, spreadsheet.UpsertRowOutput{Action: "inserted", RowNumber: 2, UpdatedRange: "'Contacts'!A2:C2"}, output)
	mutex.Lock()
	defer mutex.Unlock()
	require.Len(t, writeKeys, 2)
	require.NotEmpty(t, writeKeys[0])
	require.Equal(t, writeKeys[0], writeKeys[1])
}
