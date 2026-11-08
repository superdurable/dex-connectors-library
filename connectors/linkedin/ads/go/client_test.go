// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

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

const testAccessToken = "synthetic-linkedin-ads-token"

var testConnection = sdkgo.ConnectionRef{Provider: "linkedin-ads", Name: "linkedin-ads"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "linkedin-ads-provider-test" }
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

func TestListAdAccountsProjectsBoundedNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/rest/adAccounts", r.URL.Path)
		require.Equal(t, "search", r.URL.Query().Get("q"))
		require.Equal(t, "2", r.URL.Query().Get("pageSize"))
		require.Equal(t, "202609", r.Header.Get("Linkedin-Version"))
		require.Equal(t, "2.0.0", r.Header.Get("X-Restli-Protocol-Version"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"elements":[{"id":507404993,"name":"Example Account","secret":"not-persisted"}],"metadata":{"nextPageToken":"next_1"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAdAccounts(), testConnection, ListAdAccountsInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListAdAccountsBranchListed, result.Branch)
	require.Equal(t, AdAccountsPage{Accounts: []AdAccount{{ID: "507404993", Name: "Example Account"}}, NextPageToken: "next_1"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListAdAccountsClassifiesFailuresAndRejectsInvalidInputWithoutRequest(t *testing.T) {
	var responseStatus atomic.Int32
	var responseBody atomic.Value
	var calls atomic.Int32
	responseBody.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(responseStatus.Load()))
		_, err := w.Write([]byte(responseBody.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		responseStatus.Store(int32(status))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListAdAccounts(), testConnection, ListAdAccountsInput{PageSize: 2})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	responseStatus.Store(http.StatusForbidden)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListAdAccounts(), testConnection, ListAdAccountsInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListAdAccountsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"elements":{}}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListAdAccounts(), testConnection, ListAdAccountsInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListAdAccountsBranchInvalidResponse, result.Branch)
	for _, input := range []ListAdAccountsInput{{PageSize: -1}, {PageSize: 1001}, {PageToken: "bad cursor"}} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListAdAccounts(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListAdAccountsBranchDefect, result.Branch)
	}
	require.Equal(t, int32(4), calls.Load())
}
