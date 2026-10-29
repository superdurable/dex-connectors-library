// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendgrid

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

var testConnection = sdkgo.ConnectionRef{Provider: "sendgrid", Name: "sendgrid"}

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

func TestGetStatsProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/stats", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		require.Equal(t, "Bearer "+testAPIKey, r.Header.Get("Authorization"))
		require.Equal(t, "2026-10-01", r.URL.Query().Get("start_date"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`[{"date":"2026-10-01","stats":[{"metrics":{"requests":8,"delivered":7}}]}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetStats(), testConnection, GetStatsInput{StartDate: "2026-10-01", EndDate: "2026-10-02"})
	require.NoError(t, err)
	require.Equal(t, GetStatsBranchFound, result.Branch)
	require.Equal(t, 7, result.Value.Days[0].Delivered)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.GetStats(), testConnection, GetStatsInput{StartDate: "2026-10-01", EndDate: "2026-10-02"})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestListTemplatesProviderRequestAndRateLimit(t *testing.T) {
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v3/templates", r.URL.Path)
		require.Equal(t, "GET", r.Method)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		_ = body
		require.Equal(t, "Bearer "+testAPIKey, r.Header.Get("Authorization"))
		require.Equal(t, "dynamic", r.URL.Query().Get("generations"))
		require.Equal(t, "100", r.URL.Query().Get("page_size"))
		if status.Load() == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			_, err = w.Write([]byte("private provider text"))
			require.NoError(t, err)
			return
		}
		_, err = w.Write([]byte(`{"result":[{"id":"d-123","name":"Receipt","generation":"dynamic"}],"_metadata":{"count":1,"next":"https://api.sendgrid.com/v3/templates?page_token=next-123"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTemplates(), testConnection, ListTemplatesInput{})
	require.NoError(t, err)
	require.Equal(t, ListTemplatesBranchListed, result.Branch)
	require.Equal(t, "Receipt", result.Value.Templates[0].Name)
	require.Equal(t, "next-123", result.Value.NextPageToken)
	status.Store(1)
	_, err = sdkgo.RunQuery(queryContext{}, client.ListTemplates(), testConnection, ListTemplatesInput{})
	var retry *dex.RetryAfterError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2*time.Second, retry.After)
	require.NotContains(t, err.Error(), "private provider text")
	require.NotContains(t, err.Error(), testAPIKey)
}

func TestListTemplatesPageSelectionAndValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "200", r.URL.Query().Get("page_size"))
		require.Equal(t, "next-123", r.URL.Query().Get("page_token"))
		_, err := w.Write([]byte(`{"result":[],"_metadata":{"count":0}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTemplates(), testConnection, ListTemplatesInput{PageSize: 200, PageToken: "next-123"})
	require.NoError(t, err)
	require.Equal(t, ListTemplatesBranchListed, result.Branch)
	for _, pageSize := range []int{-1, 201} {
		result, err = sdkgo.RunQuery(queryContext{}, client.ListTemplates(), testConnection, ListTemplatesInput{PageSize: pageSize})
		require.NoError(t, err)
		require.Equal(t, ListTemplatesBranchDefect, result.Branch)
	}
}
