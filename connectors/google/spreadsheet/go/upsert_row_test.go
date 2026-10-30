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
	spreadsheet "github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

func TestUpsertUsesCredentialRefreshedByLookupForWrite(t *testing.T) {
	for _, test := range []struct {
		name     string
		rows     [][]string
		method   string
		response string
	}{
		{name: "append", rows: [][]string{{"accountId", "name"}}, method: http.MethodPost, response: `{"updates":{"updatedRange":"'Customers'!A2:B2"}}`},
		{name: "replacement", rows: [][]string{{"accountId", "name"}, {"a-1", "Old"}}, method: http.MethodPut, response: `{"updatedRange":"'Customers'!A2:B2"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes int
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					if request.Header.Get("Authorization") == "Bearer rejected-token" {
						response.WriteHeader(http.StatusUnauthorized)
						return
					}
					require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": test.rows}))
					return
				}
				writes++
				require.Equal(t, test.method, request.Method)
				require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
				_, _ = response.Write([]byte(test.response))
			}))
			t.Cleanup(server.Close)
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, provider)
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("sheet-flow", "lookup-refresh-"+test.name), client.UpsertRow(), sheetsConnection, upsertInput)
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
			require.Equal(t, 1, provider.forcedRefreshes)
			require.Equal(t, 1, writes)
		})
	}
}

func TestUpsertWriteUnauthorizedRefreshesWithoutUnsafeAppendResend(t *testing.T) {
	for _, test := range []struct {
		name       string
		rows       [][]string
		method     string
		wantWrites int
	}{
		{name: "append", rows: [][]string{{"accountId", "name"}}, method: http.MethodPost, wantWrites: 2},
		{name: "replacement", rows: [][]string{{"accountId", "name"}, {"a-1", "Old"}}, method: http.MethodPut, wantWrites: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var writes int
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": test.rows}))
					return
				}
				writes++
				require.Equal(t, test.method, request.Method)
				if writes == 1 {
					require.Equal(t, "Bearer rejected-token", request.Header.Get("Authorization"))
					response.WriteHeader(http.StatusUnauthorized)
					return
				}
				require.Equal(t, "Bearer replacement-token", request.Header.Get("Authorization"))
				if test.method == http.MethodPost {
					_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
				} else {
					_, _ = response.Write([]byte(`{"updatedRange":"'Customers'!A2:B2"}`))
				}
			}))
			t.Cleanup(server.Close)
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, provider)
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("sheet-flow", "write-refresh-"+test.name)
			result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			if test.method == http.MethodPost {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 1, writes)
				ctx.AdvanceAttempt()
				result, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			}
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
			require.Equal(t, 1, provider.forcedRefreshes)
			require.Equal(t, test.wantWrites, writes)
		})
	}
}

func TestUpsertAppendUnauthorizedWithoutNewCredentialIsRejected(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": rows}))
			return
		}
		writes++
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	provider := &rejectionRefreshingCredentialProvider{keepRejected: true}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("sheet-flow", "unchanged-token"), client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchProviderRejected, result.Branch)
	require.Equal(t, 1, provider.forcedRefreshes)
	require.Equal(t, 1, writes)
}

func TestUpsertUnauthorizedRefreshFailureExplainsCredentialRecovery(t *testing.T) {
	for _, test := range []struct {
		name         string
		rows         [][]string
		refreshError error
		wantMessage  string
	}{
		{name: "append terminal", rows: [][]string{{"accountId", "name"}}, refreshError: sdkgo.NewReauthorizationRequiredError(errors.New("consent revoked")), wantMessage: "reconnect"},
		{name: "append transient", rows: [][]string{{"accountId", "name"}}, refreshError: errors.New("token endpoint unavailable"), wantMessage: "temporarily unavailable"},
		{name: "replacement terminal", rows: [][]string{{"accountId", "name"}, {"a-1", "Old"}}, refreshError: sdkgo.NewReauthorizationRequiredError(errors.New("consent revoked")), wantMessage: "reconnect"},
		{name: "replacement transient", rows: [][]string{{"accountId", "name"}, {"a-1", "Old"}}, refreshError: errors.New("token endpoint unavailable"), wantMessage: "temporarily unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": test.rows}))
					return
				}
				writes++
				response.WriteHeader(http.StatusUnauthorized)
			}))
			t.Cleanup(server.Close)
			credentials := &rejectionRefreshingCredentialProvider{refreshError: test.refreshError}
			client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, credentials)
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("sheet-flow", "401-"+test.name), client.UpsertRow(), sheetsConnection, upsertInput)
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.wantMessage)
			require.Equal(t, 1, credentials.forcedRefreshes)
			require.Equal(t, 1, writes)
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

func TestUpsertClaimedAppendWithMismatchedRowIsUncertain(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		rows = append(rows, []string{"a-1", "Different"})
		response.WriteHeader(http.StatusInternalServerError)
		return true
	})
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "claimed-mismatch")
	first, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, second.Branch)
	require.Equal(t, 1, writes)
}

func TestUpsertClaimedAppendWithChangedSchemaIsUncertain(t *testing.T) {
	for _, test := range []struct {
		name string
		rows [][]string
	}{
		{name: "missing key", rows: [][]string{{"name"}, {"Ada"}}},
		{name: "missing input column", rows: [][]string{{"accountId"}, {"a-1"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			reads := 0
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					reads++
					rows := [][]string{{"accountId", "name"}}
					if reads > 2 {
						rows = test.rows
					}
					require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": rows}))
					return
				}
				writes++
				response.WriteHeader(http.StatusInternalServerError)
			}))
			t.Cleanup(server.Close)
			client := newSheetsClient(t, server.URL)
			ctx := singledispatchtest.NewContext("sheet-flow", "changed-schema-"+test.name)
			first, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUncertain, first.Branch)
			ctx.AdvanceAttempt()
			second, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			require.NoError(t, err)
			require.Equal(t, spreadsheet.UpsertRowBranchUncertain, second.Branch)
			require.Equal(t, 1, writes)
		})
	}
}

func TestUpsertAppendRangeWithExclamationInSheetName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = response.Write([]byte(`{"values":[["accountId","name"]]}`))
			return
		}
		_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Q1!2024'!A5:B5"}}`))
	}))
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	input := upsertInput
	input.SheetName = "Q1!2024"
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("sheet-flow", "quoted-range"), client.UpsertRow(), sheetsConnection, input)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, int64(5), result.Value.RowNumber)
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

type terminalCredentialSource struct{}

func (terminalCredentialSource) Resolve(sdkgo.Call) (spreadsheet.Credentials, error) {
	return spreadsheet.Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("reauthorization required"))
}

func (source terminalCredentialSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[spreadsheet.Credentials]) (spreadsheet.Credentials, error) {
	return source.Resolve(call)
}

func TestUpsertTerminalCredentialFailureRecordsNotSentThenDefect(t *testing.T) {
	client, err := spreadsheet.New(spreadsheet.Config{}, terminalCredentialSource{})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "terminal-credential")
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchDefect, result.Branch)
	frames := ctx.Heartbeats()
	require.Len(t, frames, 1)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[0], &marker))
	require.Equal(t, singledispatch.StateNotSent, marker.State)
}

type laterCredentialFailureSource struct {
	attempts int
	failure  error
}

func (source *laterCredentialFailureSource) Resolve(sdkgo.Call) (spreadsheet.Credentials, error) {
	source.attempts++
	if source.attempts > 1 {
		return spreadsheet.Credentials{}, source.failure
	}
	return spreadsheet.Credentials{AccessToken: sdkgo.NewSecretString("sheets-token")}, nil
}

func (source *laterCredentialFailureSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[spreadsheet.Credentials]) (spreadsheet.Credentials, error) {
	return source.Resolve(call)
}

func TestUpsertClaimedAppendCredentialFailureNeverReportsDefectOrResends(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   error
		uncertain bool
	}{
		{name: "terminal", failure: sdkgo.NewReauthorizationRequiredError(errors.New("reauthorization required")), uncertain: true},
		{name: "transient", failure: errors.New("credential refresh unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			rows := [][]string{{"accountId", "name"}}
			writes := 0
			server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
				writes++
				rows = append(rows, []string{"a-1", "Ada"})
				_, _ = response.Write([]byte(`{"updates":`))
				return true
			})
			originalHandler := server.Config.Handler
			server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet && writes > 0 {
					response.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				originalHandler.ServeHTTP(response, request)
			})
			t.Cleanup(server.Close)
			credentials := &laterCredentialFailureSource{failure: test.failure}
			client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, credentials)
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("sheet-flow", "claimed-credential-"+test.name)
			_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, 1, writes)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
			if test.uncertain {
				require.NoError(t, err)
				require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, "reconnect")
			} else {
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}
			require.Equal(t, 1, writes)
		})
	}
}

func TestUpsertTerminalCredentialFailureRetainsSavedProviderRange(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
		return true
	})
	t.Cleanup(server.Close)
	credentials := &laterCredentialFailureSource{failure: sdkgo.NewReauthorizationRequiredError(errors.New("reauthorization required"))}
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "saved-provider-range")
	first, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, second.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, second.Failure.Kind)
	require.Equal(t, "'Customers'!A2:B2", second.Receipt.ProviderObjectID)
	require.Equal(t, 1, writes)
}

func TestUpsertMissingMarkerTerminalCredentialFailureIsUncertain(t *testing.T) {
	client, err := spreadsheet.New(spreadsheet.Config{}, terminalCredentialSource{})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "missing-marker-credential")
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, ctx.Heartbeats())
}

func TestUpsertLookupFailureRecordsNotSentBeforeNextAttempt(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	lookups := 0
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			lookups++
			if lookups == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": rows}))
			return
		}
		writes++
		_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
	}))
	t.Cleanup(server.Close)
	client := newSheetsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("sheet-flow", "lookup-retry")
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 0, writes)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, 1, writes)
}

func TestUpsertFailClosedOnHeartbeatReadError(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		return true
	})
	t.Cleanup(server.Close)
	ctx := singledispatchtest.NewContext("sheet-flow", "read-error")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	result, err := sdkgo.RunMutation(ctx, newSheetsClient(t, server.URL).UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUncertain, result.Branch)
	require.Equal(t, 0, writes)
}

func TestUpsertRetrySeesRowInsertedBeforeSecondAttempt(t *testing.T) {
	rows := [][]string{{"accountId", "name"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		rows = append(rows, []string{"a-1", "Ada"})
		response.WriteHeader(http.StatusServiceUnavailable)
		return true
	})
	t.Cleanup(server.Close)
	ctx := singledispatchtest.NewContext("sheet-flow", "delayed-row")
	client := newSheetsClient(t, server.URL)
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "inserted", result.Value.Action)
	require.Equal(t, 1, writes)
}

func TestUpsertReadBackWithoutMarkerReportsFoundRowAsUpdated(t *testing.T) {
	rows := [][]string{{"accountId", "name"}, {"a-1", "Ada"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, _ *http.Request) bool {
		writes++
		return true
	})
	t.Cleanup(server.Close)
	ctx := singledispatchtest.NewContext("sheet-flow", "missing-marker")
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, newSheetsClient(t, server.URL).UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "updated", result.Value.Action)
	require.Equal(t, 0, writes)
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
	var posts atomic.Int32
	server := sheetServer(t, &rows, nil)
	t.Cleanup(server.Close)
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(&http.Client{Transport: sheetRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts.Add(1)
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
	require.Equal(t, int32(1), posts.Load())
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.ErrorAs(t, err, &retry)
	require.Equal(t, int32(2), posts.Load())
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

func TestUpsertDeadlineCutsAppendAndReadBackMissIsUncertain(t *testing.T) {
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

func TestUpsertBoundsLookupAndReadBackBeforeStepDeadline(t *testing.T) {
	reads := 0
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			reads++
			if reads == 2 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			rows := [][]string{{"accountId", "name"}}
			if reads == 3 {
				rows = append(rows, []string{"a-1", "Ada"})
			}
			require.NoError(t, json.NewEncoder(response).Encode(map[string]any{"values": rows}))
			return
		}
		writes++
		response.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	var requestDeadlines []time.Time
	client, err := spreadsheet.New(spreadsheet.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[spreadsheet.Credentials]{
		sheetsConnection: {AccessToken: sdkgo.NewSecretString("sheets-token")},
	}, spreadsheet.WithHTTPClient(&http.Client{Transport: sheetRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet {
			deadline, hasDeadline := request.Context().Deadline()
			require.True(t, hasDeadline)
			requestDeadlines = append(requestDeadlines, deadline)
		}
		return http.DefaultTransport.RoundTrip(request)
	})}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "bounded-readback")
	stepDeadline := time.Now().Add(2 * time.Second)
	cancel := ctx.SetDeadline(stepDeadline)
	defer cancel()
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 1, writes)
	require.Len(t, requestDeadlines, 2)
	for _, requestDeadline := range requestDeadlines {
		require.Greater(t, stepDeadline.Sub(requestDeadline), 100*time.Millisecond)
	}
	ctx.AdvanceAttempt()
	cancelNext := ctx.SetDeadline(time.Now().Add(2 * time.Second))
	defer cancelNext()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "inserted", result.Value.Action)
	require.Equal(t, 1, writes)
}

type blockingCredentialSource struct{}

func (blockingCredentialSource) Resolve(sdkgo.Call) (spreadsheet.Credentials, error) {
	return spreadsheet.Credentials{}, errors.New("credential resolution unavailable")
}

func (blockingCredentialSource) ResolveWithRefresh(ctx context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[spreadsheet.Credentials]) (spreadsheet.Credentials, error) {
	<-ctx.Done()
	return spreadsheet.Credentials{}, ctx.Err()
}

func TestUpsertBoundsCredentialResolutionBeforeStepDeadline(t *testing.T) {
	client, err := spreadsheet.New(spreadsheet.Config{}, blockingCredentialSource{})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("sheet-flow", "bounded-credential")
	stepDeadline := time.Now().Add(time.Second)
	cancel := ctx.SetDeadline(stepDeadline)
	defer cancel()
	_, err = sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Greater(t, time.Until(stepDeadline), 50*time.Millisecond)
	frames := ctx.Heartbeats()
	require.Len(t, frames, 1)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[0], &marker))
	require.Equal(t, singledispatch.StateNotSent, marker.State)
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

func TestUpsertAppliedReplacement500RetryReportsUpdated(t *testing.T) {
	rows := [][]string{{"accountId", "name"}, {"a-1", "Old"}}
	writes := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
		require.Equal(t, http.MethodPut, request.Method)
		writes++
		rows[1] = []string{"a-1", "Ada"}
		response.WriteHeader(http.StatusInternalServerError)
		return true
	})
	t.Cleanup(server.Close)
	ctx := singledispatchtest.NewContext("sheet-flow", "applied-replacement-retry")
	client := newSheetsClient(t, server.URL)
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	frames := ctx.Heartbeats()
	require.Len(t, frames, 1)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[0], &marker))
	require.Equal(t, singledispatch.StateNotSent, marker.State)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "updated", result.Value.Action)
	require.Equal(t, 1, writes)
}

func TestUpsertReplacementRetryCanAppendAfterRowDeletion(t *testing.T) {
	rows := [][]string{{"accountId", "name"}, {"a-1", "Old"}}
	puts := 0
	posts := 0
	server := sheetServer(t, &rows, func(response http.ResponseWriter, request *http.Request) bool {
		switch request.Method {
		case http.MethodPut:
			puts++
			rows = rows[:1]
			response.WriteHeader(http.StatusServiceUnavailable)
		case http.MethodPost:
			posts++
			_, _ = response.Write([]byte(`{"updates":{"updatedRange":"'Customers'!A2:B2"}}`))
		default:
			t.Errorf("unexpected write method %s", request.Method)
		}
		return true
	})
	t.Cleanup(server.Close)
	ctx := singledispatchtest.NewContext("sheet-flow", "replacement-row-deleted")
	client := newSheetsClient(t, server.URL)
	_, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.UpsertRow(), sheetsConnection, upsertInput)
	require.NoError(t, err)
	require.Equal(t, spreadsheet.UpsertRowBranchUpserted, result.Branch)
	require.Equal(t, "inserted", result.Value.Action)
	require.Equal(t, 1, puts)
	require.Equal(t, 1, posts)
}

var _ context.Context = (*singledispatchtest.Context)(nil)
