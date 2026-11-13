// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gowu

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "gowu", Name: "gowu"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "gowu-provider-test" }
func (queryContext) StepExecutionID() string     { return "access-client" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: "test-app-key", AppSecret: sdkgo.NewSecretString("private-secret")}})
	require.NoError(t, err)
	return client
}
func runAccess(t *testing.T, server *httptest.Server) (sdkgo.QueryResult[AccessClientResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetAccessClient(), testConnection, GetAccessClientInput{})
}
func TestV2SignatureVector(t *testing.T) {
	require.Equal(t, "7b652d44fd016d1e613a0ba7328cb9216c5bdb44d348f72d689e3d986b1a87c7", signAccessClient("private-secret", "1700000000", "0123456789abcdef0123456789abcdef"))
}
func TestGetAccessClientSignedRequestAndPrivateOutput(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/access-clients/me", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "test-app-key", r.Header.Get("x-app-key"))
		require.Equal(t, "2", r.Header.Get("x-sign-version"))
		require.Len(t, r.Header.Get("x-nonce"), 32)
		_, err := strconv.ParseInt(r.Header.Get("x-timestamp"), 10, 64)
		require.NoError(t, err)
		require.Equal(t, signAccessClient("private-secret", r.Header.Get("x-timestamp"), r.Header.Get("x-nonce")), r.Header.Get("x-signature"))
		_, err = w.Write([]byte(`{"appKey":"test-app-key","remark":"Example integration","createdAt":"2026-01-01T00:00:00Z","apiCallCount":42,"secret":"private-secret"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runAccess(t, server)
	require.NoError(t, err)
	require.Equal(t, GetAccessClientBranchFound, result.Branch)
	require.Equal(t, AccessClientResult{Remark: "Example integration", APICallCount: 42}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "test-app-key")
	require.NotContains(t, string(encoded), "private-secret")
	require.EqualValues(t, 1, calls.Load())
}
func TestGetAccessClientOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"auth rejection", 401, `{"code":"INVALID_SIGNATURE"}`, GetAccessClientBranchProviderRejected, false},
		{"rate limit", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"bad JSON", 200, `{`, GetAccessClientBranchInvalidResponse, false},
		{"wrong app", 200, `{"appKey":"other","apiCallCount":3}`, GetAccessClientBranchInvalidResponse, false},
		{"missing count", 200, `{"appKey":"test-app-key"}`, GetAccessClientBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runAccess(t, server)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
				if result.Failure != nil {
					require.NotContains(t, result.Failure.Message, "private-secret")
				}
			}
		})
	}
}
func TestInvalidCredentialsNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: "test-app-key", AppSecret: sdkgo.NewSecretString("")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetAccessClient(), testConnection, GetAccessClientInput{})
	require.NoError(t, err)
	require.Equal(t, GetAccessClientBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardAuthorization(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := runAccess(t, server)
	require.NoError(t, err)
	require.Equal(t, GetAccessClientBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
}
