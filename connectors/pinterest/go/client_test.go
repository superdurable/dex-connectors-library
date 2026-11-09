// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pinterest

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

const testAccessToken = "synthetic-pinterest-token"

var testConnection = sdkgo.ConnectionRef{Provider: "pinterest", Name: "pinterest"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "pinterest-provider-test" }
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

func TestListPinsProjectsOneCursorPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v5/pins", r.URL.Path)
		require.Equal(t, "2", r.URL.Query().Get("page_size"))
		require.Equal(t, "next page", r.URL.Query().Get("bookmark"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"items":[{"id":"123456789","board_id":"987654321","title":"Example Pin","description":"not-persisted"}],"bookmark":"further"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{PageSize: 2, Bookmark: "next page"})
	require.NoError(t, err)
	require.Equal(t, ListPinsBranchListed, result.Branch)
	require.Equal(t, PinsPage{Pins: []Pin{{ID: "123456789", BoardID: "987654321", Title: "Example Pin"}}, NextBookmark: "further"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	require.NotContains(t, string(encoded), testAccessToken)
}

func TestListPinsRetriesOversizedRetryStatusBodies(t *testing.T) {
	var responseStatus atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(int(responseStatus.Load()))
		_, err := w.Write([]byte(`{"code":8,"message":"response body larger than the configured limit"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	credentials := Credentials{
		OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}
	client, err := New(Config{BaseURL: server.URL, MaxResponseBytes: 16}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	for status, kind := range map[int]sdkgo.FailureKind{http.StatusTooManyRequests: sdkgo.FailureRateLimit, http.StatusServiceUnavailable: sdkgo.FailureAvailability} {
		responseStatus.Store(int32(status))
		_, err := sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, kind, retry.Failure.Kind)
	}
	responseStatus.Store(http.StatusOK)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPinsBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestListPinsClassifiesFailuresAndRejectsInvalidInputWithoutRequest(t *testing.T) {
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
		_, err := sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testAccessToken)
	}
	responseStatus.Store(http.StatusForbidden)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPinsBranchProviderRejected, result.Branch)
	responseStatus.Store(http.StatusOK)
	responseBody.Store(`{"items":{}}`)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, ListPinsInput{})
	require.NoError(t, err)
	require.Equal(t, ListPinsBranchInvalidResponse, result.Branch)
	for _, input := range []ListPinsInput{{PageSize: 251}, {Bookmark: "a\nb"}} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListPins(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListPinsBranchDefect, result.Branch)
	}
	require.Equal(t, int32(4), calls.Load())
}
