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

func TestUpdateAndDeleteJobUseBoundedRequests(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		paths = append(paths, request.URL.Path)
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "Bearer sample-databricks-api-token", request.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, float64(123), body["job_id"])
		if request.URL.Path == "/api/2.2/jobs/update" {
			require.Equal(t, map[string]any{"name": "Dex renamed job"}, body["new_settings"])
		}
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateJobName(), testConnection, UpdateJobNameInput{JobID: "123", Name: "Dex renamed job"})
	require.NoError(t, err)
	require.Equal(t, UpdateJobNameBranchUpdated, updated.Branch)
	require.Equal(t, JobSummary{ID: "123", Name: "Dex renamed job"}, updated.Value)
	deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteJob(), testConnection, DeleteJobInput{JobID: "123"})
	require.NoError(t, err)
	require.Equal(t, DeleteJobBranchDeleted, deleted.Branch)
	require.Equal(t, DeletedJob{ID: "123"}, deleted.Value)
	require.Equal(t, []string{"/api/2.2/jobs/update", "/api/2.2/jobs/delete"}, paths)
}

func TestUpdateAndDeleteJobClassifyRejectionAndRetries(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(status) }))
		client := newTestClient(t, server.URL, 0)
		updated, updateErr := sdkgo.RunMutation(queryContext{}, client.UpdateJobName(), testConnection, UpdateJobNameInput{JobID: "123", Name: "Dex renamed job"})
		deleted, deleteErr := sdkgo.RunMutation(queryContext{}, client.DeleteJob(), testConnection, DeleteJobInput{JobID: "123"})
		if status == http.StatusTooManyRequests || status >= 500 {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, updateErr, &retry)
			require.ErrorAs(t, deleteErr, &retry)
		} else {
			require.NoError(t, updateErr)
			require.NoError(t, deleteErr)
			if status == http.StatusNotFound {
				require.Equal(t, UpdateJobNameBranchNotFound, updated.Branch)
				require.Equal(t, DeleteJobBranchNotFound, deleted.Branch)
			} else {
				require.Equal(t, UpdateJobNameBranchProviderRejected, updated.Branch)
				require.Equal(t, DeleteJobBranchProviderRejected, deleted.Branch)
			}
			require.NotContains(t, updated.Failure.Message, "sample-databricks-api-token")
			require.NotContains(t, deleted.Failure.Message, "sample-databricks-api-token")
		}
		server.Close()
	}
}
