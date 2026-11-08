// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package aweber

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-aweber-token"

var testConnection = sdkgo.ConnectionRef{Provider: "aweber", Name: "aweber"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "aweber-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListAccountsProjectsProviderPageAndCredentialsStaySecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/1.0/accounts", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("ws.start"))
		require.Equal(t, "3", r.URL.Query().Get("ws.size"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"entries":[{"id":123,"company":"Example Company","analytics_src":"not-persisted"}],"start":2,"total_size":3}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchListed, result.Branch)
	require.Equal(t, AccountsPage{Accounts: []Account{{ID: "123", Company: "Example Company"}}, Offset: 2, Limit: 3, Total: 3}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListAccountsClassifiesRateLimitAndOtherFailures(t *testing.T) {
	var responseStatus atomic.Int32
	var responseBody atomic.Value
	var calls atomic.Int32
	responseBody.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(int(responseStatus.Load()))
		_, err := w.Write([]byte(responseBody.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	responseStatus.Store(http.StatusForbidden)
	responseBody.Store(`{"error":{"type":"ForbiddenError","message":"Rate Limit Error"}}`)
	_, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAccessToken)
	responseBody.Store(`{"error":{"type":"ForbiddenError","message":"Account suspended"}}`)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusServiceUnavailable)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	require.ErrorAs(t, err, &retry)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"entries":{},"total_size":1}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListAccounts(), testConnection, ListAccountsInput{Limit: 101})
	require.NoError(t, err)
	require.Equal(t, ListAccountsBranchDefect, result.Branch)
	require.Equal(t, int32(4), calls.Load())
}
