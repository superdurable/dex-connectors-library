// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailgun

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAPIKey = "test-key-123"

var testConnection = sdkgo.ConnectionRef{Provider: "mailgun", Name: "mailgun"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
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

func TestGetEventsProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/example.test/events", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "api", user)
		require.Equal(t, testAPIKey, password)
		require.Equal(t, "20", r.URL.Query().Get("limit"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"items":[{"id":"event1","event":"delivered","timestamp":1793100000}],"paging":{"next":"https://api.mailgun.net/next"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetEvents(), testConnection, GetEventsInput{Domain: "example.test", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, GetEventsBranchListed, result.Branch)
	require.Equal(t, "delivered", result.Value.Events[0].Event)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetEvents(), testConnection, GetEventsInput{Domain: "example.test", Limit: 20})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestListDomainsProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v4/domains", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		user, password, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "api", user)
		require.Equal(t, testAPIKey, password)
		require.Equal(t, "2", r.URL.Query().Get("skip"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"total_count":1,"items":[{"name":"example.test","state":"active"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListDomains(), testConnection, ListDomainsInput{Limit: 10, Skip: 2})
	require.NoError(t, err)
	require.Equal(t, ListDomainsBranchListed, result.Branch)
	require.Equal(t, "example.test", result.Value.Domains[0].Name)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListDomains(), testConnection, ListDomainsInput{Limit: 10, Skip: 2})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}
