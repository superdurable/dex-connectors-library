// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var upsertInput = spreadsheet.UpsertRowInput{
	SpreadsheetID: "sheet", SheetName: "Customers", KeyColumn: "accountId", KeyValue: "a-1",
	Values: map[string]string{"accountId": "a-1", "name": "Ada"},
}

func TestUpsertRejectedAppendRetriesWithOneSendPerAttempt(t *testing.T) {
	for _, test := range []struct {
		name     string
		status   int
		wantKind sdkgo.FailureKind
	}{
		{name: "rate limit", status: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit},
		{name: "unavailable", status: http.StatusServiceUnavailable, wantKind: sdkgo.FailureAvailability},
		{name: "conflict", status: http.StatusConflict, wantKind: sdkgo.FailureConflict},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := [][]string{{"accountId", "name"}}
			var keys []string
			server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
				require.Equal(t, http.MethodPost, request.Method)
				keys = append(keys, request.Header.Get("X-Idempotency-Key"))
				if len(keys) == 1 {
					response.WriteHeader(test.status)
					return true
				}
				rows = append(rows, []string{"a-1", "Ada"})
				_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
				return true
			})
			t.Cleanup(server.Close)
			client := newSheetsClient(t, server.URL)
			ctx := singledispatchtest.NewContext("sheet-flow", "upsert-row")

			_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.wantKind, retry.Failure.Kind)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
			require.Equal(t, spreadsheet.UpsertRowOutput{Action: "inserted", RowNumber: 2, UpdatedRange: "'Customers'!A2:B2"}, result.Value)
			require.Len(t, keys, 2)
			require.NotEmpty(t, keys[0])
			require.Equal(t, keys[0], keys[1])
		})
	}
}

func TestUpsertAcceptedAppendReadBackNeverResends(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      string
		firstRead int
		wantRetry bool
		maxBytes  int64
	}{
		{name: "server error after acceptance", status: http.StatusInternalServerError},
		{name: "unparsable success", status: http.StatusOK, body: `{"updates":`, firstRead: http.StatusServiceUnavailable, wantRetry: true},
		{name: "success without range", status: http.StatusOK, body: `{"updates":{}}`},
		{name: "oversized success", status: http.StatusOK, body: strings.Repeat("x", 256), maxBytes: 128},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := [][]string{{"accountId", "name"}}
			writes := 0
			readsAfterWrite := 0
			server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
				writes++
				rows = append(rows, []string{"a-1", "Ada"})
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
				return true
			})
			originalHandler := server.Config.Handler
			server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet && writes > 0 {
					readsAfterWrite++
					if readsAfterWrite == 1 && test.firstRead != 0 {
						response.WriteHeader(test.firstRead)
						return
					}
				}
				originalHandler.ServeHTTP(response, request)
			})
			t.Cleanup(server.Close)
			client := newSheetsClient(t, server.URL)
			if test.maxBytes > 0 {
				var err error
				client, err = spreadsheet.New(spreadsheet.Config{Endpoint: server.URL, MaxResponseBytes: test.maxBytes}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
					sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
				})
				require.NoError(t, err)
			}
			ctx := singledispatchtest.NewContext("sheet-flow", "accepted-append")
			result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				ctx.AdvanceAttempt()
				result, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			}
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
			require.Equal(t, "inserted", result.Value.Action)
			require.Equal(t, 1, writes)
		})
	}
}

func TestUpsertClaimLossFailsClosedWithoutSecondAppend(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		response.WriteHeader(http.StatusInternalServerError)
		return true
	})
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "lost-claim")
	ctx.LoseNextHeartbeat()
	first, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, second.Branch)
	require.Equal(t, 1, writes)
}

type oneFailureCredentialSource struct{ attempts int }

func (source *oneFailureCredentialSource) Resolve(sdkgo.Call) (spreadsheet.Credentials, error) {
	source.attempts++
	if source.attempts == 1 {
		return spreadsheet.Credentials{}, errors.New("credential unavailable")
	}
	return spreadsheet.Credentials{AccessToken: sdkgo.NewSecretString("sheets-token")}, nil
}

func (source *oneFailureCredentialSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[spreadsheet.Credentials]) (spreadsheet.Credentials, error) {
	return source.Resolve(call)
}

func TestUpsertCredentialFailureRecordsNotSentBeforeRetry(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
		return true
	})
	t.Cleanup(server.Close)
	source := &oneFailureCredentialSource{}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, source)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "credential-retry")
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 0, writes)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, 1, writes)
}

func TestUpsertCheckpointFailureBeforeSendRetries(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		return true
	})
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "checkpoint-failure")
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 0, writes)
}

type sheetRoundTripperFunc func(*http.Request) (*http.Response, error)

func (roundTrip sheetRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

type interruptedWriteBody struct{}

func (interruptedWriteBody) Read([]byte) (int, error) { return 0, errors.New("interrupted response") }

func TestUpsertTracedConnectFailureAllowsRetry(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	server := sheetServer(t, &rows, nil)
	t.Cleanup(server.Close)
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(&http.Client{Transport: sheetRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.ConnectDone != nil {
				trace.ConnectDone("tcp", "unreachable", errors.New("connect refused"))
			}
			return nil, errors.New("connect refused")
		}
		return http.DefaultTransport.RoundTrip(request)
	})}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "connect-failure")
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.ErrorAs(t, err, &retry)
}

func TestUpsertInterruptedSuccessBodyUsesReadBackWithoutResend(t *testing.T) {
	var accepted atomic.Bool
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		rows := [][]string{{"accountId", "name"}}
		if accepted.Load() {
			rows = append(rows, []string{"a-1", "Ada"})
		}
		require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"range": "Customers", "values": rows}))
	}))
	t.Cleanup(server.Close)
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(&http.Client{Transport: sheetRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			writes.Add(1)
			accepted.Store(true)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(interruptedWriteBody{}), Request: request}, nil
		}
		return http.DefaultTransport.RoundTrip(request)
	})}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "interrupted-body")
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "inserted", result.Value.Action)
	require.Equal(t, int32(1), writes.Load())
}

func TestUpsertDroppedReplyOnReusedConnectionIsNotReplayed(t *testing.T) {
	var mutex sync.Mutex
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write([]byte(`{"range":"Customers","majorDimension":"ROWS","values":[["accountId","name"]]}`))
			return
		}
		mutex.Lock()
		writes++
		mutex.Unlock()
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "drop-reply")
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
	mutex.Lock()
	defer mutex.Unlock()
	require.Equal(t, 1, writes)
}

func TestUpsertDeadlineCutsAppendAndReadBackFailureIsUncertain(t *testing.T) {
	var writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = response.Write([]byte(`{"range":"Customers","values":[["accountId","name"]]}`))
			return
		}
		writes.Add(1)
		time.Sleep(350 * time.Millisecond)
		response.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "deadline")
	cancel := ctx.SetDeadline(time.Now().Add(450 * time.Millisecond))
	defer cancel()
	started := time.Now()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
	require.Equal(t, int32(1), writes.Load())
	require.Less(t, time.Since(started), time.Second)
}

func TestUpsertInvalidReadBackFailsUncertainWithoutResend(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			writes++
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		if writes > 0 {
			_, _ = response.Write([]byte(`{"values":`))
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{"range": "Customers", "values": [][]string{{"accountId", "name"}}})
	}))
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "invalid-readback")
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
	require.Equal(t, 1, writes)
}

func TestUpsertReplacement503RetriesAndAcceptedInvalidBodySucceeds(t *testing.T) {
	rows := [][]string{{"accountId", "name"}, {"a-1", "Old"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
		require.Equal(t, http.MethodPut, request.Method)
		writes++
		if writes == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			return true
		}
		rows[1] = []string{"a-1", "Ada"}
		_, _ = response.Write([]byte(`{"updatedRange":`))
		return true
	})
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "replace-retry")
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "updated", result.Value.Action)
	require.Equal(t, 2, writes)
}

var _ context.Context = (*singledispatchtest.Context)(nil)
