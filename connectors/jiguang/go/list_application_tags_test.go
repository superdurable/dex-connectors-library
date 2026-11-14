// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiguang

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "jiguang", Name: "jiguang"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "jiguang-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-application-tags" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxBytes int64) *Client {
	t.Helper()
	credential := Credentials{AppKey: "7d431e42dfa6a6d693ac2d04", MasterSecret: sdkgo.NewSecretString("5e987ac6d2e04d95a9d8f0d1")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credential})
	require.NoError(t, err)
	return client
}
func TestListApplicationTagsUsesBasicAuthAndReadsNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/v3/tags/", request.URL.Path)
		require.Empty(t, request.URL.RawQuery)
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.Equal(t, "Basic N2Q0MzFlNDJkZmE2YTZkNjkzYWMyZDA0OjVlOTg3YWM2ZDJlMDRkOTVhOWQ4ZjBkMQ==", request.Header.Get("Authorization"))
		_, err := io.WriteString(writer, `{"tags":["news","中文标签"]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListApplicationTags(), testConnection, ListApplicationTagsInput{})
	require.NoError(t, err)
	require.Equal(t, ListApplicationTagsBranchListed, result.Branch)
	require.Equal(t, ApplicationTags{Tags: []string{"news", "中文标签"}}, result.Value)
}
func TestListApplicationTagsClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"empty array", 200, `{"tags":[]}`, ListApplicationTagsBranchListed, false},
		{"null tags", 200, `{"tags":null}`, ListApplicationTagsBranchListed, false},
		{"unauthorized", 401, `{"error":{"code":2001}}`, ListApplicationTagsBranchProviderRejected, false},
		{"blacklisted", 403, `{"error":{"code":2003}}`, ListApplicationTagsBranchProviderRejected, false},
		{"not found", 404, `{}`, ListApplicationTagsBranchProviderRejected, false},
		{"rate limited", 429, `{"error":{"code":2002}}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"missing tags", 200, `{}`, ListApplicationTagsBranchInvalidResponse, false},
		{"wrong type", 200, `{"tags":"news"}`, ListApplicationTagsBranchInvalidResponse, false},
		{"empty tag", 200, `{"tags":[""]}`, ListApplicationTagsBranchInvalidResponse, false},
		{"too long", 200, `{"tags":["abcdefghijklmnopqrstuvwxyz1234567890abcdef"]}`, ListApplicationTagsBranchInvalidResponse, false},
		{"malformed", 200, `{"tags":`, ListApplicationTagsBranchInvalidResponse, false},
		{"trailing JSON", 200, `{"tags":[]} {}`, ListApplicationTagsBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(tc.status)
				_, err := io.WriteString(writer, tc.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListApplicationTags(), testConnection, ListApplicationTagsInput{})
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
func TestListApplicationTagsUsesRateResetAndBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("X-Rate-Limit-Reset", "23")
		writer.WriteHeader(429)
	}))
	client := newTestClient(t, server.URL, 0)
	response := client.listTags(sdkgo.Call{Context: queryContext{}, Connection: testConnection})
	require.Equal(t, 23*time.Second, response.retryAfter)
	server.Close()
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := io.WriteString(writer, `{"tags":["news"]}`)
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 5).ListApplicationTags(), testConnection, ListApplicationTagsInput{})
	require.NoError(t, err)
	require.Equal(t, ListApplicationTagsBranchInvalidResponse, result.Branch)
	endpoint := server.URL
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, endpoint, 0).ListApplicationTags(), testConnection, ListApplicationTagsInput{})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
func TestListApplicationTagsRejectsUnsafeConfigurationAndCredentials(t *testing.T) {
	_, err := New(Config{BaseURL: "https://device.jpush.cn/v3/tags"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
	for _, value := range []string{"", "bad:name", "bad\nvalue", " leading"} {
		require.False(t, validAppKey(value))
	}
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListApplicationTags(), testConnection, ListApplicationTagsInput{})
	require.NoError(t, err)
	require.Equal(t, ListApplicationTagsBranchDefect, result.Branch)
}
