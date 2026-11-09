// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pubsub

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

const testAccessToken = "synthetic-google-pubsub-token"

var testConnection = sdkgo.ConnectionRef{Provider: "google-pubsub", Name: "google-pubsub"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "google-pubsub-provider-test" }
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

func TestListTopicsProjectsOneCursorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/projects/sample-project/topics", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("pageSize"))
		require.Equal(t, "next page", r.URL.Query().Get("pageToken"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"topics":[{"name":"projects/sample-project/topics/events","labels":{"secret":"not-persisted"}}],"nextPageToken":"further"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, ListTopicsInput{ProjectID: "sample-project", PageSize: 2, PageToken: "next page"})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchListed, result.Branch)
	require.Equal(t, TopicsPage{Topics: []Topic{{Name: "projects/sample-project/topics/events"}}, NextPageToken: "further"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListTopicsClassifiesFailuresAndRejectsInvalidInputWithoutRequest(t *testing.T) {
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
		_, err := sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, ListTopicsInput{ProjectID: "sample-project"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	responseStatus.Store(http.StatusForbidden)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, ListTopicsInput{ProjectID: "sample-project"})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"topics":{}}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, ListTopicsInput{ProjectID: "sample-project"})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchInvalidResponse, result.Branch)
	for _, input := range []ListTopicsInput{{ProjectID: "sample-project", PageSize: 101}, {ProjectID: "sample-project", PageToken: "a\nb"}, {ProjectID: ""}} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListTopicsBranchDefect, result.Branch)
	}
	require.Equal(t, int32(4), calls.Load())
}
