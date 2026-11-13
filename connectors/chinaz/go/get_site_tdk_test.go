// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chinaz

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "chinaz", Name: "chinaz"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "chinaz-provider-test" }
func (queryContext) StepExecutionID() string     { return "site-tdk" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("secretTestKey")}})
	require.NoError(t, err)
	return client
}
func TestGetSiteTDKRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/single/tdk", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.NoError(t, r.ParseMultipartForm(4096))
		require.Equal(t, "secretTestKey", r.FormValue("key"))
		require.Equal(t, "example.com", r.FormValue("domain"))
		_, err := w.Write([]byte(`{"StateCode":1,"Reason":"成功","Result":{"Title":"Example Site","Keywords":"example","Description":"Public example","IP":"192.0.2.1"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetSiteTDK(), testConnection, GetSiteTDKInput{Domain: "example.com"})
	require.NoError(t, err)
	require.Equal(t, GetSiteTDKBranchFound, result.Branch)
	require.Equal(t, SiteTDK{Domain: "example.com", Title: "Example Site", Keywords: "example", Description: "Public example", IPAddress: "192.0.2.1"}, result.Value)
	require.EqualValues(t, 1, calls.Load())
}
func TestGetSiteTDKOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"not found", 200, `{"StateCode":10025}`, GetSiteTDKBranchNotFound, false},
		{"invalid key", 200, `{"StateCode":10001}`, GetSiteTDKBranchProviderRejected, false},
		{"plan exhausted", 200, `{"StateCode":10022}`, GetSiteTDKBranchProviderRejected, false},
		{"rate limited", 200, `{"StateCode":10026}`, "", true},
		{"gateway unavailable", 200, `{"StateCode":10029}`, "", true},
		{"http rate limit", 429, `{}`, "", true},
		{"http unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, GetSiteTDKBranchInvalidResponse, false},
		{"missing result", 200, `{"StateCode":1}`, GetSiteTDKBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetSiteTDK(), testConnection, GetSiteTDKInput{Domain: "example.com"})
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidInputAndKeyNeverCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetSiteTDK(), testConnection, GetSiteTDKInput{Domain: "https://example.com"})
	require.NoError(t, err)
	require.Equal(t, GetSiteTDKBranchDefect, result.Branch)
	invalid, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("bad key")}})
	require.NoError(t, err)
	result, err = sdkgo.RunQuery(queryContext{}, invalid.GetSiteTDK(), testConnection, GetSiteTDKInput{Domain: "example.com"})
	require.NoError(t, err)
	require.Equal(t, GetSiteTDKBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardKey(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).GetSiteTDK(), testConnection, GetSiteTDKInput{Domain: "example.com"})
	require.NoError(t, err)
	require.Equal(t, GetSiteTDKBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.NotContains(t, result.Failure.Message, "secretTestKey")
	require.False(t, strings.Contains(result.Failure.Message, "key="))
}
