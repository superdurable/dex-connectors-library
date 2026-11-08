// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package instagrambusiness

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

const testUserToken = "synthetic-instagram-token"

var testConnection = sdkgo.ConnectionRef{Provider: "instagram-business", Name: "instagram-business"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "instagram-business-provider-test" }
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

func TestListMediaProjectsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/17900000000000001/media", r.URL.Path)
		require.Equal(t, "id,caption,media_type,timestamp,permalink", r.URL.Query().Get("fields"))
		require.Equal(t, "100", r.URL.Query().Get("limit"))
		require.Equal(t, "cursor/+value", r.URL.Query().Get("after"))
		require.Equal(t, "Bearer "+testUserToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"data":[{"id":"17900000000000001","caption":"Hello","media_type":"IMAGE","timestamp":"2026-11-08T10:00:00+0000","permalink":"https://www.instagram.com/p/example/","comments":{"data":[{"text":"private"}]}}],"paging":{"cursors":{"after":"next-cursor"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListMedia(), testConnection, ListMediaInput{IGUserID: "17900000000000001", After: "cursor/+value"})
	require.NoError(t, err)
	require.Equal(t, ListMediaBranchListed, result.Branch)
	require.Equal(t, "next-cursor", result.Value.NextCursor)
	require.Equal(t, "Hello", result.Value.Media[0].Caption)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testUserToken)
}

func TestListMediaFailures(t *testing.T) {
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
	invalid, err := sdkgo.RunQuery(queryContext{}, client.ListMedia(), testConnection, ListMediaInput{IGUserID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, ListMediaBranchDefect, invalid.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListMedia(), testConnection, ListMediaInput{IGUserID: "17900000000000001"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testUserToken)
	}
	status.Store(http.StatusNotFound)
	missing, err := sdkgo.RunQuery(queryContext{}, client.ListMedia(), testConnection, ListMediaInput{IGUserID: "17900000000000001"})
	require.NoError(t, err)
	require.Equal(t, ListMediaBranchNotFound, missing.Branch)
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListMedia(), testConnection, ListMediaInput{IGUserID: "17900000000000001"})
	require.NoError(t, err)
	require.Equal(t, ListMediaBranchProviderRejected, rejected.Branch)
}

func TestListMediaRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(`{"data":[{"id":"bad/id","media_type":"IMAGE","timestamp":"2026-11-08T10:00:00+0000"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListMedia(), testConnection, ListMediaInput{IGUserID: "17900000000000001"})
	require.NoError(t, err)
	require.Equal(t, ListMediaBranchInvalidResponse, result.Branch)
}
