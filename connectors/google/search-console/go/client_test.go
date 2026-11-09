// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package searchconsole

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

const testAccessToken = "synthetic-search-console-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-search-console", Name: "google-search-console"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "search-console-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	credentials := Credentials{OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token")}
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestListSitesProjectsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/webmasters/v3/sites", r.URL.Path)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.RawQuery)
		_, err := w.Write([]byte(`{"siteEntry":[{"siteUrl":"sc-domain:example.com","permissionLevel":"siteOwner","internal":"private"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSites(), testConnection, ListSitesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSitesBranchListed, result.Branch)
	require.Equal(t, "sc-domain:example.com", result.Value.Sites[0].SiteURL)
	require.Equal(t, "siteOwner", result.Value.Sites[0].PermissionLevel)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListSitesClassifiesFailures(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{"errors":[{"message":"unavailable"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListSites(), testConnection, ListSitesInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListSites(), testConnection, ListSitesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSitesBranchProviderRejected, rejected.Branch)
	require.Equal(t, int32(3), calls.Load())
}

func TestListSitesRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"siteEntry":[{"siteUrl":"sc-domain:example.com"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSites(), testConnection, ListSitesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSitesBranchInvalidResponse, result.Branch)
}
