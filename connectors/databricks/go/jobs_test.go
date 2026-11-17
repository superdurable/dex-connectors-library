// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package databricks

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListJobsReadsBoundedPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/api/2.2/jobs/list", request.URL.Path)
		require.Equal(t, "Example job", request.URL.Query().Get("name"))
		require.Equal(t, "25", request.URL.Query().Get("limit"))
		require.Equal(t, "next", request.URL.Query().Get("page_token"))
		require.Equal(t, "Bearer sample-databricks-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"jobs":[{"job_id":11223344,"settings":{"name":"Example job","email_notifications":{"secret":"excluded"}}}],"next_page_token":"after","workspace_secret":"excluded"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListJobs(), testConnection, ListJobsInput{Name: "Example job", Limit: 25, PageToken: "next"})
	require.NoError(t, err)
	require.Equal(t, ListJobsBranchListed, result.Branch)
	require.Equal(t, JobPage{Jobs: []JobSummary{{ID: "11223344", Name: "Example job"}}, NextPageToken: "after"}, result.Value)
}

func TestGetJobAndListJobRunsReadOnlySelectedFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/2.2/jobs/get":
			require.Equal(t, "11223344", request.URL.Query().Get("job_id"))
			_, err := writer.Write([]byte(`{"job_id":11223344,"settings":{"name":"Example job","tasks":[{"secret":"excluded"}]}}`))
			require.NoError(t, err)
		case "/api/2.2/jobs/runs/list":
			require.Equal(t, "11223344", request.URL.Query().Get("job_id"))
			require.Equal(t, "10", request.URL.Query().Get("limit"))
			_, err := writer.Write([]byte(`{"runs":[{"run_id":455644833,"status":{"state":"RUNNING"},"tasks":[{"secret":"excluded"}]}],"next_page_token":"later"}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected route %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	job, err := sdkgo.RunQuery(queryContext{}, client.GetJob(), testConnection, GetJobInput{JobID: "11223344"})
	require.NoError(t, err)
	require.Equal(t, JobSummary{ID: "11223344", Name: "Example job"}, job.Value)
	runs, err := sdkgo.RunQuery(queryContext{}, client.ListJobRuns(), testConnection, ListJobRunsInput{JobID: "11223344", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, JobRunPage{Runs: []JobRunStatus{{ID: "455644833", Status: "RUNNING"}}, NextPageToken: "later"}, runs.Value)
}

func TestRunJobAndCancelRunUseBoundedRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		switch request.URL.Path {
		case "/api/2.2/jobs/run-now":
			require.Equal(t, float64(11223344), body["job_id"])
			require.NotEmpty(t, body["idempotency_token"])
			_, err := writer.Write([]byte(`{"run_id":455644833,"workspace_secret":"excluded"}`))
			require.NoError(t, err)
		case "/api/2.2/jobs/runs/cancel":
			require.Equal(t, float64(455644833), body["run_id"])
			_, err := writer.Write([]byte(`{}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected route %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	started, err := sdkgo.RunMutation(queryContext{}, client.RunJob(), testConnection, RunJobInput{JobID: "11223344"})
	require.NoError(t, err)
	require.Equal(t, RunJobBranchStarted, started.Branch)
	require.Equal(t, StartedJobRun{RunID: "455644833"}, started.Value)
	cancelled, err := sdkgo.RunMutation(queryContext{}, client.CancelJobRun(), testConnection, CancelJobRunInput{RunID: "455644833"})
	require.NoError(t, err)
	require.Equal(t, CancelJobRunBranchCancelled, cancelled.Branch)
}

func TestJobOperationsClassifyProviderFailures(t *testing.T) {
	for _, testCase := range []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{
		{status: 404, branch: GetJobBranchNotFound},
		{status: 403, branch: GetJobBranchProviderRejected},
		{status: 429, retry: true},
		{status: 503, retry: true},
		{status: 500, retry: true},
		{status: 200, branch: GetJobBranchInvalidResponse},
	} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, _ = writer.Write([]byte(`{"secret":"excluded"}`))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetJob(), testConnection, GetJobInput{JobID: "11223344"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "excluded")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}
