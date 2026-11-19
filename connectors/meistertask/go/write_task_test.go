// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meistertask

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestUpdateTaskSendsAbsoluteFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPut, request.Method)
		require.Equal(t, "/tasks/15", request.URL.Path)
		require.Equal(t, "Bearer sample-meistertask-api-token", request.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]any{"name": "Updated Task Name"}, body)
		_, err := writer.Write([]byte(`{"id":15,"status":1,"name":"Updated Task Name"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	name := "Updated Task Name"
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "15", Name: &name})
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUpdated, result.Branch)
	require.Equal(t, name, result.Value.Name)
}

func TestUpdateTaskAssignsMovesAndSetsDue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]any{"assigned_to_id": float64(7), "section_id": float64(9), "due": "2026-11-18T00:00:00Z"}, body)
		_, err := writer.Write([]byte(`{"id":15,"status":1,"name":"Moved Task"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	assigneeID, sectionID, due := int64(7), int64(9), "2026-11-18T00:00:00Z"
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "15", AssignedToID: &assigneeID, SectionID: &sectionID, Due: &due})
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchUpdated, result.Branch)
	invalidDue := "tomorrow"
	invalid, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "15", Due: &invalidDue})
	require.NoError(t, err)
	require.Equal(t, UpdateTaskBranchDefect, invalid.Branch)
}

func TestUpdateTaskClassifiesOutcomes(t *testing.T) {
	name := "Updated Task Name"
	for _, testCase := range []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{{404, UpdateTaskBranchNotFound, false}, {403, UpdateTaskBranchProviderRejected, false}, {429, "", true}, {503, "", true}, {200, UpdateTaskBranchInvalidResponse, false}} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
			}))
			defer server.Close()
			result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).UpdateTask(), testConnection, UpdateTaskInput{TaskID: "15", Name: &name})
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

func TestCreateTaskBoundsUnknownResends(t *testing.T) {
	requests := 0
	status := http.StatusInternalServerError
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/sections/7/tasks", request.URL.Path)
		require.Equal(t, "Bearer sample-meistertask-api-token", request.Header.Get("Authorization"))
		var body map[string]any
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, map[string]any{"name": "Task Name", "notes": "Details"}, body)
		writer.WriteHeader(status)
		if status == http.StatusOK {
			_, err := writer.Write([]byte(`{"id":15,"status":1,"name":"Task Name"}`))
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	ctx := singledispatchtest.NewContext("meistertask-create", "create-task")
	client := newTestClient(t, server.URL, 0)
	input := CreateTaskInput{SectionID: "7", Name: "Task Name", Notes: "Details"}
	for attempt := 0; attempt < 2; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreateTask(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreateTask(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateTaskBranchUncertain, result.Branch)
	require.Equal(t, 3, requests)
}

func TestCreateTaskSucceedsAndRejects(t *testing.T) {
	for _, testCase := range []struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{{200, `{"id":15,"status":1,"name":"Task Name"}`, CreateTaskBranchCreated}, {400, `{}`, CreateTaskBranchProviderRejected}} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			ctx := singledispatchtest.NewContext("meistertask-create-branch", http.StatusText(testCase.status))
			result, err := sdkgo.RunMutation(ctx, newTestClient(t, server.URL, 0).CreateTask(), testConnection, CreateTaskInput{SectionID: "7", Name: "Task Name"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}
