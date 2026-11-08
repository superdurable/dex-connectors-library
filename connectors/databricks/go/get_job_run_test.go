// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

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

var testConnection = sdkgo.ConnectionRef{Provider: "databricks", Name: "databricks"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "databricks-provider-test" }
func (queryContext) StepExecutionID() string     { return "get-job_run" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIToken: sdkgo.NewSecretString("sample-databricks-api-token")}
	client, err := New(Config{BaseURL: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestGetJobRunReadsSelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/2.2/jobs/runs/get", request.URL.Path)
		require.Equal(t, "455644833", request.URL.Query().Get("run_id"))
		require.Equal(t, "Bearer sample-databricks-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"run_id":455644833,"status":{"state":"RUNNING"},"customer_id":"private"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetJobRun(), testConnection, GetJobRunInput{RunID: "455644833"})
	require.NoError(t, err)
	require.Equal(t, GetJobRunBranchFound, result.Branch)
	require.Equal(t, JobRunStatus{ID: "455644833", Status: "RUNNING", ResultState: ""}, result.Value)
}

func TestGetJobRunClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, branch: GetJobRunBranchNotFound},
		{name: "forbidden", status: 403, branch: GetJobRunBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `{"run_id":"other","status":{"state":"RUNNING"}}`, branch: GetJobRunBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetJobRun(), testConnection, GetJobRunInput{RunID: "455644833"})
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

func TestGetJobRunRejectsInvalidIDAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://dbc-example.cloud.databricks.com", 0)
	for _, runID := range []string{"", "../other", "bad\njob_run", string([]byte{0xff})} {
		result, err := sdkgo.RunQuery(queryContext{}, client.GetJobRun(), testConnection, GetJobRunInput{RunID: runID})
		require.NoError(t, err)
		require.Equal(t, GetJobRunBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`{"run_id":455644833,"status":{"state":"RUNNING"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 20).GetJobRun(), testConnection, GetJobRunInput{RunID: "455644833"})
	require.NoError(t, err)
	require.Equal(t, GetJobRunBranchInvalidResponse, result.Branch)
}
