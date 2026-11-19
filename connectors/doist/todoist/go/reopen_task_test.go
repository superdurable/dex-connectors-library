// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package todoist

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestReopenTaskUsesV1PathAndClassifiesOutcomes(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		branch sdkgo.BranchID
	}{
		"reopened": {http.StatusOK, ReopenTaskBranchReopened},
		"missing":  {http.StatusNotFound, ReopenTaskBranchNotFound},
		"denied":   {http.StatusForbidden, ReopenTaskBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodPost, request.Method)
				require.Equal(t, "/api/v1/tasks/6XGgmFVcrG5RRjVr/reopen", request.URL.Path)
				require.Equal(t, "Bearer "+testToken, request.Header.Get("Authorization"))
				response.WriteHeader(testCase.status)
				if testCase.status == http.StatusOK {
					_, _ = response.Write([]byte("null"))
				}
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/v1")
			result, err := sdkgo.RunMutation(providerContext{}, client.ReopenTask(), testConnection, ReopenTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if result.Failure == nil {
				require.Equal(t, "6XGgmFVcrG5RRjVr", result.Value.TaskID)
			} else {
				require.NotContains(t, result.Failure.Message, testToken)
			}
		})
	}
}

func TestReopenTaskRetriesRateLimitAndUnknownServerResult(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { response.WriteHeader(status) }))
		client := newTestClient(t, server.URL+"/api/v1")
		_, err := sdkgo.RunMutation(providerContext{}, client.ReopenTask(), testConnection, ReopenTaskInput{TaskID: "6XGgmFVcrG5RRjVr"})
		require.Error(t, err)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, retry.Failure.Message, testToken)
		server.Close()
	}
}
