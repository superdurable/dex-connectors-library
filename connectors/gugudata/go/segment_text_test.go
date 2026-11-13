// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gugudata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAppKey = "test-gugudata-app-key"

var testConnection = sdkgo.ConnectionRef{Provider: "gugudata", Name: "gugudata"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "gugudata-provider-test" }
func (queryContext) StepExecutionID() string     { return "segment-text" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

type redirectTransport struct{ serverURL *url.URL }

func (transport redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	copied := request.Clone(request.Context())
	copied.URL.Scheme = transport.serverURL.Scheme
	copied.URL.Host = transport.serverURL.Host
	copied.Host = transport.serverURL.Host
	return http.DefaultTransport.RoundTrip(copied)
}
func newTestClient(t *testing.T, serverURL string, maxResponseBytes int64) *Client {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	require.NoError(t, err)
	credentials := sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AppKey: sdkgo.NewSecretString(testAppKey)}}
	client, err := New(Config{MaxResponseBytes: maxResponseBytes}, credentials, WithHTTPClient(&http.Client{Transport: redirectTransport{parsed}}))
	require.NoError(t, err)
	return client
}
func TestSegmentTextRequestAndResponse(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/text/segment", request.URL.Path)
		require.Equal(t, "计算机科学", request.URL.Query().Get("text"))
		require.Empty(t, request.URL.Query().Get("appkey"))
		require.Equal(t, testAppKey, request.Header.Get("X-GUGUDATA-APPKEY"))
		_, err := writer.Write([]byte(`{"DataStatus":{"StatusCode":100},"Data":{"SegmentaryText":"计算机,科学"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SegmentText(), testConnection, SegmentTextInput{Text: "计算机科学"})
	require.NoError(t, err)
	require.Equal(t, SegmentTextBranchSegmented, result.Branch)
	require.Equal(t, "计算机,科学", result.Value.SegmentedText)
	require.EqualValues(t, 1, calls.Load())
}
func TestSegmentTextOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"invalid key", 401, `private provider data`, SegmentTextBranchProviderRejected, false},
		{"provider rejection", 200, `{"DataStatus":{"StatusCode":504}}`, SegmentTextBranchProviderRejected, false},
		{"http rate limit", 429, `private provider data`, "", true},
		{"server failure", 503, `private provider data`, "", true},
		{"malformed", 200, `{`, SegmentTextBranchInvalidResponse, false},
		{"missing data", 200, `{"DataStatus":{"StatusCode":100}}`, SegmentTextBranchInvalidResponse, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Retry-After", "2")
				writer.WriteHeader(tc.status)
				_, err := writer.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SegmentText(), testConnection, SegmentTextInput{Text: "计算机科学"})
			if tc.retry {
				var retry *dex.RetryAfterError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 2*time.Second, retry.After)
				require.NotContains(t, err.Error(), testAppKey)
				require.NotContains(t, err.Error(), "private provider data")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestInvalidTextSendsNoRequest(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { calls.Add(1); writer.WriteHeader(http.StatusOK) }))
	defer server.Close()
	for _, text := range []string{"", "  ", strings.Repeat("x", 2001), "\xff"} {
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SegmentText(), testConnection, SegmentTextInput{Text: text})
		require.NoError(t, err)
		require.Equal(t, SegmentTextBranchDefect, result.Branch)
	}
	require.Zero(t, calls.Load())
}
func TestOversizedResponseAndTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(strings.Repeat("x", 100)))
		require.NoError(t, err)
	}))
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).SegmentText(), testConnection, SegmentTextInput{Text: "计算机科学"})
	require.NoError(t, err)
	require.Equal(t, SegmentTextBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
	server.Close()
	_, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SegmentText(), testConnection, SegmentTextInput{Text: "计算机科学"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testAppKey)
}
