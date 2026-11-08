// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kit

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

const testAPIKey = "test-secret-kit-key"

var testConnection = sdkgo.ConnectionRef{Provider: "kit", Name: "kit"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "kit-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
	require.NoError(t, err)
	return client
}

func TestListSubscribersTokenPaginationAndRateLimit(t *testing.T) {
	var rateLimited atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v4/subscribers", r.URL.Path)
		require.Equal(t, "cursor-1", r.URL.Query().Get("after"))
		require.Equal(t, "3", r.URL.Query().Get("per_page"))
		require.Equal(t, "true", r.URL.Query().Get("slim"))
		require.Equal(t, testAPIKey, r.Header.Get("X-Kit-Api-Key"))
		if rateLimited.Load() {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, err := w.Write([]byte(`{"subscribers":[{"id":204,"email_address":"alice@example.com","first_name":"Alice","state":"active","fields":{"private":"not-persisted"}}],"pagination":{"has_next_page":true,"end_cursor":"cursor-2","per_page":3}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v4")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSubscribers(), testConnection, ListSubscribersInput{After: "cursor-1", PerPage: 3})
	require.NoError(t, err)
	require.Equal(t, ListSubscribersBranchListed, result.Branch)
	require.Equal(t, "cursor-2", result.Value.NextCursor)
	require.Equal(t, int64(204), result.Value.Subscribers[0].ID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	rateLimited.Store(true)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListSubscribers(), testConnection, ListSubscribersInput{After: "cursor-1", PerPage: 3})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestGetSubscriberExcludesFieldsAndClassifiesMissing(t *testing.T) {
	var missing atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v4/subscribers/204", r.URL.Path)
		require.Equal(t, testAPIKey, r.Header.Get("X-Kit-Api-Key"))
		if missing.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, err := w.Write([]byte(`{"subscriber":{"id":204,"email_address":"alice@example.com","first_name":"Alice","state":"active","fields":{"private":"not-persisted"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v4")
	result, err := sdkgo.RunQuery(queryContext{}, client.GetSubscriber(), testConnection, GetSubscriberInput{SubscriberID: 204})
	require.NoError(t, err)
	require.Equal(t, GetSubscriberBranchFound, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "not-persisted")
	missing.Store(true)
	result, err = sdkgo.RunQuery(queryContext{}, client.GetSubscriber(), testConnection, GetSubscriberInput{SubscriberID: 204})
	require.NoError(t, err)
	require.Equal(t, GetSubscriberBranchNotFound, result.Branch)
}

func TestListSubscribersMalformedAndInvalidInput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, err := w.Write([]byte(`{"subscribers":{},"pagination":{"has_next_page":false}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v4")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSubscribers(), testConnection, ListSubscribersInput{})
	require.NoError(t, err)
	require.Equal(t, ListSubscribersBranchInvalidResponse, result.Branch)
	result, err = sdkgo.RunQuery(queryContext{}, client.ListSubscribers(), testConnection, ListSubscribersInput{PerPage: 1001})
	require.NoError(t, err)
	require.Equal(t, ListSubscribersBranchDefect, result.Branch)
	require.Equal(t, int32(1), calls.Load())
}
