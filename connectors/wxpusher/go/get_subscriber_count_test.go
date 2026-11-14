// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wxpusher

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

var testConnection = sdkgo.ConnectionRef{Provider: "wxpusher", Name: "wxpusher"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "wxpusher-provider-test" }
func (queryContext) StepExecutionID() string     { return "subscriber-count" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint, token string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppToken: sdkgo.NewSecretString(token)}})
	require.NoError(t, err)
	return client
}
func runCount(t *testing.T, server *httptest.Server, token string) (sdkgo.QueryResult[SubscriberCountResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, token).GetSubscriberCount(), testConnection, GetSubscriberCountInput{})
}
func TestGetSubscriberCountRequestAndPrivateOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/api/fun/wxuser/v2", r.URL.Path)
		require.Equal(t, "AT_dummy", r.URL.Query().Get("appToken"))
		require.Equal(t, "1", r.URL.Query().Get("page"))
		require.Equal(t, "1", r.URL.Query().Get("pageSize"))
		_, err := w.Write([]byte(`{"code":1000,"msg":"处理成功","data":{"total":40,"page":1,"pageSize":1,"records":[{"uid":"UID_private","nickName":"private user"}]},"success":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runCount(t, server, "AT_dummy")
	require.NoError(t, err)
	require.Equal(t, GetSubscriberCountBranchCounted, result.Branch)
	require.Equal(t, SubscriberCountResult{Total: 40}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "UID_private")
	require.NotContains(t, string(encoded), "private user")
}
func TestGetSubscriberCountOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"rejected", 200, `{"code":1001,"msg":"bad token"}`, GetSubscriberCountBranchProviderRejected, false},
		{"limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetSubscriberCountBranchInvalidResponse, false},
		{"missing total", 200, `{"code":1000,"success":true,"data":{"page":1,"pageSize":1,"records":[]}}`, GetSubscriberCountBranchInvalidResponse, false},
		{"wrong page", 200, `{"code":1000,"success":true,"data":{"total":40,"page":2,"pageSize":1,"records":[]}}`, GetSubscriberCountBranchInvalidResponse, false},
		{"unauthorized", 401, `{}`, GetSubscriberCountBranchProviderRejected, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runCount(t, server, "AT_dummy")
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidTokenDoesNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := runCount(t, server, "invalid")
	require.NoError(t, err)
	require.Equal(t, GetSubscriberCountBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardToken(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runCount(t, server, "AT_dummy")
	require.NoError(t, err)
	require.Equal(t, GetSubscriberCountBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "AT_dummy")
}
