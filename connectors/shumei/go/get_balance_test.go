// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package shumei

import (
	"context"
	"encoding/json"
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

var testConnection = sdkgo.ConnectionRef{Provider: "shumei", Name: "shumei"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "shumei-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString("secretTestKey")}})
	require.NoError(t, err)
	return client
}
func TestGetBalanceRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/saas/balance/v1", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Contains(t, r.Header.Get("Content-Type"), "application/json")
		var input map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		require.Equal(t, map[string]any{"accessKey": "secretTestKey"}, input)
		_, err := w.Write([]byte(`{"code":1100,"message":"成功","content":{"balance":1357.65}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchRetrieved, result.Branch)
	require.Equal(t, 1357.65, result.Value.BalanceYuan)
	require.EqualValues(t, 1, calls.Load())
}
func TestGetBalanceOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"zero balance", 200, `{"code":1100,"message":"成功","content":{"balance":0}}`, GetBalanceBranchRetrieved, false},
		{"invalid accessKey", 401, `{}`, GetBalanceBranchProviderRejected, false},
		{"forbidden", 403, `{}`, GetBalanceBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"provider rejection", 200, `{"code":1901,"message":"invalid key"}`, GetBalanceBranchProviderRejected, false},
		{"malformed", 200, `{`, GetBalanceBranchInvalidResponse, false},
		{"missing content", 200, `{"code":1100}`, GetBalanceBranchInvalidResponse, false},
		{"missing balance", 200, `{"code":1100,"content":{}}`, GetBalanceBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidAccessKeyNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString("bad key")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectNeverForwardsAccessKey(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestKey")
	require.False(t, strings.Contains(result.Failure.Message, "accessKey"))
}
