// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListTasksRequestsFilteredPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/tasks", request.URL.Path)
		require.Equal(t, "2", request.URL.Query().Get("page"))
		require.Equal(t, "10", request.URL.Query().Get("items"))
		require.Equal(t, "open", request.URL.Query().Get("status"))
		require.Equal(t, "Bearer sample-meistertask-api-token", request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`[{"id":15,"status":1,"name":"Task Name","notes":"private"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTasks(), testConnection, ListTasksInput{Page: 2, Items: 10, Status: "open"})
	require.NoError(t, err)
	require.Equal(t, ListTasksBranchListed, result.Branch)
	require.Equal(t, TasksPage{Tasks: []TaskSummary{{ID: "15", Name: "Task Name", Status: "1"}}, Page: 2}, result.Value)
}

func TestListTasksClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "forbidden", status: 403, branch: ListTasksBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "malformed", status: 200, body: `[{"id":"wrong"}]`, branch: ListTasksBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTasks(), testConnection, ListTasksInput{Page: 2, Items: 10, Status: "open"})
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

func TestListTasksRejectsInvalidInput(t *testing.T) {
	client := newTestClient(t, "https://www.meistertask.com/api", 0)
	for _, input := range []ListTasksInput{{Page: -1}, {Items: 101}, {Status: "unknown"}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.ListTasks(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, ListTasksBranchDefect, result.Branch)
	}
}
