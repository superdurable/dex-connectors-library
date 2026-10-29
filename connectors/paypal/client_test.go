// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paypal

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "paypal", Name: "paypal"}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: Credentials{ClientID: "client-test", ClientSecret: sdkgo.NewSecretString("secret-test")}})
	require.NoError(t, err)
	return client
}

func TestGetOrderReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			user, password, hasBasic := r.BasicAuth()
			require.True(t, hasBasic)
			require.Equal(t, "client-test", user)
			require.Equal(t, "secret-test", password)
			require.Equal(t, "grant_type=client_credentials", readBody(t, r))
			_, err := fmt.Fprint(w, `{"access_token":"short-lived-token","token_type":"Bearer"}`)
			require.NoError(t, err)
			return
		}
		require.Equal(t, "Bearer short-lived-token", r.Header.Get("Authorization"))
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v2/checkout/orders/ORDER101", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"id\":\"ORDER101\",\"status\":\"COMPLETED\"}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetOrder(), testConnection, GetOrderInput{OrderID: "ORDER101"})
	require.NoError(t, err)
	require.Equal(t, GetOrderBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	require.NotEmpty(t, result.Value.ID)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetOrder(), testConnection, GetOrderInput{OrderID: "ORDER101"})
	require.NoError(t, err)
	require.Equal(t, GetOrderBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func TestListTransactionsReadsAndClassifiesProviderStatus(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/oauth2/token" {
			user, password, hasBasic := r.BasicAuth()
			require.True(t, hasBasic)
			require.Equal(t, "client-test", user)
			require.Equal(t, "secret-test", password)
			require.Equal(t, "grant_type=client_credentials", readBody(t, r))
			_, err := fmt.Fprint(w, `{"access_token":"short-lived-token","token_type":"Bearer"}`)
			require.NoError(t, err)
			return
		}
		require.Equal(t, "Bearer short-lived-token", r.Header.Get("Authorization"))
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/reporting/transactions", r.URL.Path)
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, err := fmt.Fprint(w, `{"error":"SENTINEL"}`)
			require.NoError(t, err)
			return
		}
		_, err := fmt.Fprint(w, "{\"transaction_details\":[{\"transaction_info\":{\"transaction_id\":\"TXN101\",\"transaction_status\":\"S\",\"transaction_amount\":{\"currency_code\":\"USD\",\"value\":\"29.00\"}}}],\"page\":1,\"total_pages\":1}")
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTransactions(), testConnection, ListTransactionsInput{StartDate: "2026-10-01T00:00:00Z", EndDate: "2026-10-02T00:00:00Z"})
	require.NoError(t, err)
	require.Equal(t, ListTransactionsBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	require.Len(t, result.Value.TransactionDetails, 1)
	status = http.StatusNotFound
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListTransactions(), testConnection, ListTransactionsInput{StartDate: "2026-10-01T00:00:00Z", EndDate: "2026-10-02T00:00:00Z"})
	require.NoError(t, err)
	require.Equal(t, ListTransactionsBranchNotFound, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, "SENTINEL")
}

func readBody(t *testing.T, r *http.Request) string {
	t.Helper()
	body, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	return string(body)
}
