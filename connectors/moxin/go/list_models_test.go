// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package moxin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "moxin", Name: "moxin"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "moxin-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-models" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PlatformToken: sdkgo.NewSecretString("sk-test-token")}})
	require.NoError(t, err)
	return client
}
func TestListModelsRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/models", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "Bearer sk-test-token", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		_, err := w.Write([]byte(`{"object":"list","data":[{"id":"example-model","object":"model"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchListed, result.Branch)
	require.Equal(t, []Model{{ID: "example-model"}}, result.Value.Models)
	require.EqualValues(t, 1, calls.Load())
}
func TestListModelsOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"object":"list","data":[]}`, ListModelsBranchListed, false},
		{"invalid token", 401, `{}`, ListModelsBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListModelsBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, ListModelsBranchInvalidResponse, false},
		{"missing data", 200, `{"object":"list"}`, ListModelsBranchInvalidResponse, false},
		{"wrong object", 200, `{"object":"thing","data":[]}`, ListModelsBranchInvalidResponse, false},
		{"missing id", 200, `{"object":"list","data":[{"object":"model"}]}`, ListModelsBranchInvalidResponse, false},
		{"wrong item object", 200, `{"object":"list","data":[{"id":"x","object":"thing"}]}`, ListModelsBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListModels(), testConnection, ListModelsInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidTokenNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PlatformToken: sdkgo.NewSecretString("Bearer bad")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectNeverForwardsToken(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListModels(), testConnection, ListModelsInput{})
	require.NoError(t, err)
	require.Equal(t, ListModelsBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "sk-test-token")
	require.False(t, strings.Contains(result.Failure.Message, "Bearer"))
}
