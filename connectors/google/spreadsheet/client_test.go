// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var sheetsConnection = sdkgo.ConnectionRef{Provider: "google", Name: "customer-sheet"}

func TestFindRowDistinguishesMissingAndDuplicateKeys(t *testing.T) {
	rows := [][]string{{"accountId", "name"}, {"a-1", "Ada"}}
	server := sheetServer(t, &rows, nil)
	defer server.Close()
	client := newSheetsClient(t, server.URL)

	found, err := sdkgo.RunQuery(newDexContext("find-found"), client.FindRow(), sheetsConnection, spreadsheet.FindRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: "a-1"})
	require.NoError(t, err)
	require.Equal(t, spreadsheet.FindRowBranchFound, found.Branch)
	require.Equal(t, int64(2), found.Value.RowNumber)

	missing, err := sdkgo.RunQuery(newDexContext("find-missing"), client.FindRow(), sheetsConnection, spreadsheet.FindRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: "a-2"})
	require.NoError(t, err)
	require.Equal(t, spreadsheet.FindRowBranchNotFound, missing.Branch)

	rows = append(rows, []string{"a-1", "Duplicate"})
	conflict, err := sdkgo.RunQuery(newDexContext("find-conflict"), client.FindRow(), sheetsConnection, spreadsheet.FindRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: "a-1"})
	require.NoError(t, err)
	require.Equal(t, spreadsheet.FindRowBranchConflict, conflict.Branch)
	require.Equal(t, []int64{2, 3}, conflict.Value.ConflictingRows)
}

func TestGetValuesClassifiesAuthenticationAndRateLimit(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantRetry  bool
	}{
		{name: "authentication", status: http.StatusUnauthorized, wantBranch: spreadsheet.GetValuesBranchFailed, wantKind: sdkgo.FailureAuthentication},
		{name: "rate limit", status: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit, wantRetry: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
			}))
			defer server.Close()
			client := newSheetsClient(t, server.URL)
			result, err := sdkgo.RunQuery(newDexContext("get-values-"+test.name), client.GetValues(), sheetsConnection, spreadsheet.GetValuesInput{SpreadsheetID: "sheet", Range: "Customers!A:B"})
			if test.wantRetry {
				require.Error(t, err)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.NotNil(t, result.Failure)
			require.Equal(t, test.wantKind, result.Failure.Kind)
		})
	}
}

func TestGetValuesRejectsOversizedResponseWithoutRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"range":"Customers","values":[["accountId","name"]]}`))
	}))
	defer server.Close()
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL, MaxResponseBytes: 8}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newDexContext("oversized"), client.GetValues(), sheetsConnection, spreadsheet.GetValuesInput{SpreadsheetID: "sheet", Range: "Customers!A:B"})
	require.NoError(t, err)
	require.Equal(t, spreadsheet.GetValuesBranchFailed, result.Branch)
	require.NotNil(t, result.Failure)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestUpsertReconcilesAmbiguousAppendWithoutSecondRow(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	var writes int
	server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
		writes++
		var payload struct {
			Values [][]string `json:"values"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		if request.Method == http.MethodPost {
			rows = append(rows, payload.Values[0])
			response.WriteHeader(http.StatusInternalServerError)
			return true
		}
		rows[1] = payload.Values[0]
		_, _ = response.Write([]byte(`{"updatedRange":"'Customers'!A2:B2"}`))
		return true
	})
	defer server.Close()
	client := newSheetsClient(t, server.URL)
	input := spreadsheet.UpsertRowInput{SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: "a-1", Values: map[string]string{"name": "Ada"}}
	ctx := newDexContext("upsert-one")

	unknown, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, input)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, unknown.Branch)
	require.Len(t, rows, 2)

	recovered, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, input)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, recovered.Branch)
	require.Equal(t, "updated", recovered.Value.Action)
	require.Len(t, rows, 2)
	require.Equal(t, 2, writes)
	require.Equal(t, unknown.Receipt.CallID, recovered.Receipt.CallID)
	require.Equal(t, unknown.Receipt.IdempotencyKey, recovered.Receipt.IdempotencyKey)
}

func sheetServer(t *testing.T, rows *[][]string, write func(http.ResponseWriter, *http.Request) bool) *httptest.Server {
	t.Helper()
	var mutex sync.Mutex
	return httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		require.Equal(t, "Bearer sheets-token", request.Header.Get("Authorization"))
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("X-Goog-Request-Id", "google-request")
		if request.Method != http.MethodGet && write != nil && write(response, request) {
			return
		}
		if request.Method == http.MethodGet {
			require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"range": "Customers", "majorDimension": "ROWS", "values": *rows}))
			return
		}
		response.WriteHeader(http.StatusMethodNotAllowed)
	}))
}

func newSheetsClient(t *testing.T, endpoint string) *spreadsheet.Client {
	t.Helper()
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	})
	require.NoError(t, err)
	return client
}

type dexContext struct {
	context.Context
	step string
}

func newDexContext(step string) *dexContext {
	return &dexContext{Context: context.Background(), step: step}
}
func (*dexContext) FlowID() string                                  { return "customer-flow" }
func (*dexContext) RunID() string                                   { return "run" }
func (*dexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *dexContext) StepExecutionID() string                 { return context.step }
func (*dexContext) FromStepExecutionID() string                     { return "" }
func (*dexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*dexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*dexContext) Attempt() int32                                  { return 1 }
func (*dexContext) HasTimerFired() bool                             { return false }
func (*dexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*dexContext) WaitForMethodFailed() bool                       { return false }
func (*dexContext) RecordHeartbeat(any) error                       { return nil }
func (*dexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*dexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*dexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*dexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*dexContext)(nil)
