// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teamwork

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

var testConnection = sdkgo.ConnectionRef{Provider: "teamwork", Name: "teamwork"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "teamwork-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-task" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-teamwork-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetTaskReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/projects/api/v3/tasks/12345.json", request.URL.Path)
		require.Equal(t, "Basic c2FtcGxlLXRlYW13b3JrLWFwaS10b2tlbjo=", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"task":{"id":12345,"status":"pending","name":"Review launch","description":"private"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetTask(), testConnection, GetTaskInput{TaskID: "12345"})
	require.NoError(t, err)
	require.Equal(t, GetTaskBranchFound, result.Branch)
	require.Equal(t, TaskSummary{ID: "12345", Status: "pending", Name: "Review launch"}, result.Value)
}

func TestGetTaskClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetTaskBranchNotFound},
		{name: "forbidden", status: 403, branch: GetTaskBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"task":{"id":"other","status":"pending","name":"Review launch"}}`, branch: GetTaskBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetTask(), testConnection, GetTaskInput{TaskID: "12345"})
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

func TestGetTaskRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://example.teamwork.com", 0)
	for _, taskID := range []string{"", "../other", "bad\ntask", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetTask(), testConnection, GetTaskInput{TaskID: taskID})
		require.NoError(t, err)
		require.Equal(t, GetTaskBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"task":{"id":12345,"status":"pending","name":"Review launch"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetTask(), testConnection, GetTaskInput{TaskID: "12345"})
	require.NoError(t, err)
	require.Equal(t, GetTaskBranchInvalidResponse, result.Branch)
}
