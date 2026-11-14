// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tokenfactory

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

var testConnection = sdkgo.ConnectionRef{Provider: "tokenfactory", Name: "tokenfactory"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "tokenfactory-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-balance" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, url string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: url}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("secretTestKey")}})
	require.NoError(t, err)
	return client
}
func TestGetBalanceRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/user/balance", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "Bearer secretTestKey", r.Header.Get("Authorization"))
		require.Zero(t, r.ContentLength)
		_, err := w.Write([]byte(`{"balance":12.345678,"is_active":true,"unit":"CNY"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchRetrieved, result.Branch)
	require.Equal(t, Balance{BalanceYuan: 12.345678, IsActive: true, Unit: "CNY"}, result.Value)
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
		{"zero balance", 200, `{"balance":0,"is_active":true,"unit":"CNY"}`, GetBalanceBranchRetrieved, false},
		{"negative balance", 200, `{"balance":-0.5,"is_active":true,"unit":"CNY"}`, GetBalanceBranchRetrieved, false},
		{"invalid key", 401, `{}`, GetBalanceBranchProviderRejected, false},
		{"wrong method", 405, `{}`, GetBalanceBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 500, `{}`, "", true},
		{"malformed", 200, `{`, GetBalanceBranchInvalidResponse, false},
		{"missing balance", 200, `{"is_active":true,"unit":"CNY"}`, GetBalanceBranchInvalidResponse, false},
		{"missing status", 200, `{"balance":0,"unit":"CNY"}`, GetBalanceBranchInvalidResponse, false},
		{"wrong unit", 200, `{"balance":0,"is_active":true,"unit":"USD"}`, GetBalanceBranchInvalidResponse, false},
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
func TestInvalidAPIKeyNeverCallsProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("Bearer bad")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetBalance(), testConnection, GetBalanceInput{})
	require.NoError(t, err)
	require.Equal(t, GetBalanceBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectNeverForwardsAPIKey(t *testing.T) {
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
	require.False(t, strings.Contains(result.Failure.Message, "Bearer"))
}
