// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vika

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "vika", Name: "vika"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "vika-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-spaces" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("uskTestToken")}})
	require.NoError(t, err)
	return client
}
func TestListSpacesRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/fusion/v1/spaces", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "Bearer uskTestToken", r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"success":true,"code":200,"message":"SUCCESS","data":{"spaces":[{"id":"spczdmQDfBAn5","name":"Example Workspace","isAdmin":true}]}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSpaces(), testConnection, ListSpacesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSpacesBranchListed, result.Branch)
	require.Equal(t, "Example Workspace", result.Value.Spaces[0].Name)
	require.True(t, result.Value.Spaces[0].IsAdmin)
	require.EqualValues(t, 1, calls.Load())
}
func TestListSpacesOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty", 200, `{"success":true,"code":200,"data":{"spaces":[]}}`, ListSpacesBranchListed, false},
		{"invalid token", 401, `{}`, ListSpacesBranchProviderRejected, false},
		{"forbidden", 403, `{}`, ListSpacesBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"body rate limit", 200, `{"success":false,"code":429,"message":"too fast"}`, "", true},
		{"body rejection", 200, `{"success":false,"code":403,"message":"forbidden"}`, ListSpacesBranchProviderRejected, false},
		{"malformed", 200, `{`, ListSpacesBranchInvalidResponse, false},
		{"missing spaces", 200, `{"success":true,"code":200,"data":{}}`, ListSpacesBranchInvalidResponse, false},
		{"invalid space", 200, `{"success":true,"code":200,"data":{"spaces":[{"id":"bad","name":"Example"}]}}`, ListSpacesBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListSpaces(), testConnection, ListSpacesInput{})
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIToken: sdkgo.NewSecretString("Bearer bad")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListSpaces(), testConnection, ListSpacesInput{})
	require.NoError(t, err)
	require.Equal(t, ListSpacesBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
