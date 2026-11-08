// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wise

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

var testConnection = sdkgo.ConnectionRef{Provider: "wise", Name: "wise"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wise-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-transfer" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-wise-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetTransferReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/transfers/16521632", request.URL.Path)
		require.Equal(t, "Bearer sample-wise-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"id":16521632,"status":"incoming_payment_waiting","sourceCurrency":"EUR","customer_id":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetTransfer(), testConnection, GetTransferInput{TransferID: "16521632"})
	require.NoError(t, err)
	require.Equal(t, GetTransferBranchFound, result.Branch)
	require.Equal(t, TransferStatus{ID: "16521632", Status: "incoming_payment_waiting", SourceCurrency: "EUR"}, result.Value)
}

func TestGetTransferClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetTransferBranchNotFound},
		{name: "forbidden", status: 403, branch: GetTransferBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"id":"other","status":"incoming_payment_waiting","sourceCurrency":"EUR"}`, branch: GetTransferBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 0).GetTransfer(), testConnection, GetTransferInput{TransferID: "16521632"})
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

func TestGetTransferRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://api.wise.com/2026Q4", 0)
	for _, transferID := range []string{"", "../other", "bad\ntransfer", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTransfer(), testConnection, GetTransferInput{TransferID: transferID})
		require.NoError(t, err)
		require.Equal(t, GetTransferBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"id":16521632,"status":"incoming_payment_waiting","sourceCurrency":"EUR"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL+"/api", 20).GetTransfer(), testConnection, GetTransferInput{TransferID: "16521632"})
	require.NoError(t, err)
	require.Equal(t, GetTransferBranchInvalidResponse, result.Branch)
}
