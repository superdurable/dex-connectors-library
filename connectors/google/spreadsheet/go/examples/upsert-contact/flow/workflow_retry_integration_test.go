//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUpsertContactExampleRetries503ThroughRealDex(t *testing.T) {
	for _, test := range []struct {
		name       string
		method     string
		wantAction string
		wantWrites int
	}{
		{name: "append 503 then success", method: http.MethodPost, wantAction: "inserted", wantWrites: 2},
		{name: "applied replacement 500", method: http.MethodPut, wantAction: "updated", wantWrites: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mutex sync.Mutex
			var writeKeys []string
			rowApplied := false
			provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				mutex.Lock()
				defer mutex.Unlock()
				if request.Method == http.MethodGet {
					rows := [][]string{{"email", "name", "status"}}
					if test.method == http.MethodPut {
						name := "Old"
						if rowApplied {
							name = "Ada"
						}
						rows = append(rows, []string{"ada@example.test", name, "active"})
					}
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"range": "Contacts", "majorDimension": "ROWS", "values": rows}))
					return
				}
				require.Equal(t, test.method, request.Method)
				writeKeys = append(writeKeys, request.Header.Get("X-Idempotency-Key"))
				requestNumber := len(writeKeys)
				if requestNumber == 1 {
					if test.method == http.MethodPut {
						rowApplied = true
						response.WriteHeader(http.StatusInternalServerError)
					} else {
						response.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				require.Equal(t, http.MethodPost, test.method)
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

			serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
			if serverAddress == "" {
				serverAddress = "127.0.0.1:8801"
			}
			client, err := dex.NewClient(registry, cache, dex.ClientOptions{
				FlowServiceAddress: serverAddress,
			})
			require.NoError(t, err)
			defer func() { require.NoError(t, client.Close()) }()
			worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
			require.NoError(t, err)
			workerResult := make(chan error, 1)
			go func() { workerResult <- worker.Start() }()
			defer func() {
				stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
			}()
			registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
			registrationError := worker.WaitPullRegistered(registrationContext)
			cancelRegistration()
			require.NoError(t, registrationError)

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
			require.Equal(t, spreadsheet.UpsertRowOutput{Action: test.wantAction, RowNumber: 2, UpdatedRange: "'Contacts'!A2:C2"}, output)
			mutex.Lock()
			defer mutex.Unlock()
			require.Len(t, writeKeys, test.wantWrites)
			require.NotEmpty(t, writeKeys[0])
			if test.wantWrites == 2 {
				require.Equal(t, writeKeys[0], writeKeys[1])
			}
		})
	}
}
