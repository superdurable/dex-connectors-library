// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rabbitmq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "rabbitmq", Name: "rabbitmq"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "rabbitmq-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-queue-metrics" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{Username: "observer", Password: sdkgo.NewSecretString("sample-password")}
	client, err := New(Config{ManagementURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetQueueMetricsReadsOneQueue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/queues/%2F/work%2Fhigh", request.URL.EscapedPath())
		username, password, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "observer", username)
		require.Equal(t, "sample-password", password)
		_, err := writer.Write([]byte(`{"vhost":"/","name":"work/high","messages":3,"consumers":1,"state":"running","message_stats":{"publish":100}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	input := GetQueueMetricsInput{VirtualHost: "/", Name: "work/high"}
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetQueueMetrics(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, GetQueueMetricsBranchFound, result.Branch)
	require.Equal(t, QueueMetrics{VirtualHost: "/", Name: "work/high", Messages: 3, Consumers: 1, State: "running"}, result.Value)
}

func TestGetQueueMetricsClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "not found", status: 404, branch: GetQueueMetricsBranchNotFound},
		{name: "forbidden", status: 403, branch: GetQueueMetricsBranchProviderRejected},
		{name: "rate limited", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "statistics not collected", status: 200, body: `{"vhost":"/","name":"work","durable":true,"arguments":{}}`, retry: true},
		{name: "missing count", status: 200, body: `{"vhost":"/","name":"work","consumers":1,"state":"running"}`, retry: true},
		{name: "negative count", status: 200, body: `{"vhost":"/","name":"work","messages":-1,"consumers":1,"state":"running"}`, branch: GetQueueMetricsBranchInvalidResponse},
		{name: "empty state", status: 200, body: `{"vhost":"/","name":"work","messages":1,"consumers":1,"state":""}`, branch: GetQueueMetricsBranchInvalidResponse},
		{name: "mismatched name", status: 200, body: `{"vhost":"/","name":"other","messages":1,"consumers":1,"state":"running"}`, branch: GetQueueMetricsBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetQueueMetrics(), testConnection, GetQueueMetricsInput{VirtualHost: "/", Name: "work"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestGetQueueMetricsRejectsInvalidIdentityAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://rabbitmq.example.com", 0)
	for _, input := range []GetQueueMetricsInput{
		{VirtualHost: "", Name: "work"}, {VirtualHost: "/", Name: ""},
		{VirtualHost: "/", Name: "bad\tname"}, {VirtualHost: "/", Name: "bad\nname"},
	} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetQueueMetrics(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, GetQueueMetricsBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"vhost":"/","name":"work","messages":0,"consumers":0,"state":"running"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).GetQueueMetrics(), testConnection, GetQueueMetricsInput{VirtualHost: "/", Name: "work"})
	require.NoError(t, err)
	require.Equal(t, GetQueueMetricsBranchInvalidResponse, result.Branch)
}
