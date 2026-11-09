// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paddle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "paddle", Name: "paddle"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "paddle-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-transaction" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-paddle-api-key")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetTransactionReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/transactions/txn_01hv8wptq8987qeep44cyrewp9", request.URL.Path)
		require.Equal(t, "Bearer sample-paddle-api-key", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":{"id":"txn_01hv8wptq8987qeep44cyrewp9","status":"completed","currency_code":"USD","customer_id":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn_01hv8wptq8987qeep44cyrewp9"})
	require.NoError(t, err)
	require.Equal(t, GetTransactionBranchFound, result.Branch)
	require.Equal(t, TransactionStatus{ID: "txn_01hv8wptq8987qeep44cyrewp9", Status: "completed", CurrencyCode: "USD"}, result.Value)
}

func TestGetTransactionClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetTransactionBranchNotFound},
		{name: "forbidden", status: 403, branch: GetTransactionBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"data":{"id":"other","status":"completed","currency_code":"USD"}}`, branch: GetTransactionBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn_01hv8wptq8987qeep44cyrewp9"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetTransactionRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://sandbox-api.paddle.com", 0)
	for _, transactionID := range []string{"", "../other", "bad\ntransaction", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTransaction(), testConnection, GetTransactionInput{TransactionID: transactionID})
		require.NoError(t, err)
		require.Equal(t, GetTransactionBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"data":{"id":"txn_01hv8wptq8987qeep44cyrewp9","status":"completed","currency_code":"USD"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetTransaction(), testConnection, GetTransactionInput{TransactionID: "txn_01hv8wptq8987qeep44cyrewp9"})
	require.NoError(t, err)
	require.Equal(t, GetTransactionBranchInvalidResponse, result.Branch)
}
