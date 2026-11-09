// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package redis

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var testConnection = sdkgo.ConnectionRef{Provider: "upstash-redis", Name: "upstash-redis"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "redis-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-key" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{RestToken: sdkgo.NewSecretString("sample-redis-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}
func TestGetKeyReadsStringValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/", request.URL.Path)
		require.Equal(t, "Bearer sample-redis-token", request.Header.Get("Authorization"))
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `["GET","sample:key"]`, string(body))
		_, err = writer.Write([]byte(`{"result":"example"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetKey(), testConnection, GetKeyInput{Key: "sample:key"})
	require.NoError(t, err)
	require.Equal(t, GetKeyBranchFound, result.Branch)
	require.Equal(t, KeyLookup{Key: "sample:key", Value: "example", Exists: true}, result.Value)
}
func TestGetKeyClassifiesProviderOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 200, body: `{"result":null}`, branch: GetKeyBranchMissing},
		{name: "forbidden", status: 403, branch: GetKeyBranchProviderRejected},
		{name: "command error", status: 200, body: `{"error":"WRONGTYPE"}`, branch: GetKeyBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "wrong result type", status: 200, body: `{"result":17}`, branch: GetKeyBranchInvalidResponse},
		{name: "missing result", status: 200, body: `{}`, branch: GetKeyBranchInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetKey(), testConnection, GetKeyInput{Key: "sample:key"})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestGetKeyRejectsInvalidInputAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://example.upstash.io", 0)
	for _, input := range []GetKeyInput{{}, {Key: "bad\x00key"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetKey(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetKeyBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"result":"long"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).GetKey(), testConnection, GetKeyInput{Key: "sample:key"})
	require.NoError(t, err)
	require.Equal(t, GetKeyBranchInvalidResponse, result.Branch)
}
