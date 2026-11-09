// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package metaadsinsights

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

const testUserToken = "synthetic-marketing-token"

var testConnection = sdkgo.ConnectionRef{Provider: "meta-ads-insights", Name: "meta-ads-insights"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "meta-ads-insights-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {UserAccessToken: sdkgo.NewSecretString(testUserToken)}})
	require.NoError(t, err)
	return client
}

func TestGetAdProjectsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/123456789012345", r.URL.Path)
		require.Equal(t, "id,name,status,effective_status", r.URL.Query().Get("fields"))
		require.Equal(t, "Bearer "+testUserToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"id":"123456789012345","name":"Spring campaign image","status":"ACTIVE","effective_status":"ACTIVE","creative":{"id":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAd(), testConnection, GetAdInput{AdID: "123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetAdBranchFound, result.Branch)
	require.Equal(t, "Spring campaign image", result.Value.Name)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testUserToken)
}

func TestGetAdFailures(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{"error":{"message":"unavailable"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	invalid, err := sdkgo.RunQuery(queryContext{}, client.GetAd(), testConnection, GetAdInput{AdID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, GetAdBranchDefect, invalid.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.GetAd(), testConnection, GetAdInput{AdID: "123456789012345"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testUserToken)
	}
	status.Store(http.StatusNotFound)
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetAd(), testConnection, GetAdInput{AdID: "123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetAdBranchNotFound, missing.Branch)
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.GetAd(), testConnection, GetAdInput{AdID: "123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetAdBranchProviderRejected, rejected.Branch)
}

func TestGetAdRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"id":"bad/id","name":"Broken","status":"ACTIVE","effective_status":"ACTIVE"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAd(), testConnection, GetAdInput{AdID: "123456789012345"})
	require.NoError(t, err)
	require.Equal(t, GetAdBranchInvalidResponse, result.Branch)
}
