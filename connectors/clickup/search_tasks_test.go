// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSearchTasksSendsEveryFilterWithClickUpParameterNames(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"tasks":[`+taskJSON(t, testTask{id: testTaskID, name: "Fix login", assigneeIDs: []int64{183}, tags: []string{"escalated"}})+`],"last_page":true}`)
	})
	updatedAfter := time.UnixMilli(1767225600123)
	result, err := sdkgo.RunQuery(newTestDexContext("search"), newClickUpClient(t, provider.URL).SearchTasks(), clickupConnection, clickup.SearchTasksInput{
		WorkspaceID: testWorkspaceID, ListIDs: []string{testListID, "901410000002"}, FolderIDs: []string{"90140000077"}, SpaceIDs: []string{"90140000011"},
		Statuses: []string{"in progress", "blocked"}, AssigneeIDs: []int64{183, 184}, Tags: []string{"escalated"},
		UpdatedAfter: &updatedAfter, IsClosedIncluded: true, AreSubtasksIncluded: true, OrderBy: clickup.TaskOrderUpdated, IsReverse: true, Page: 2,
	})
	require.NoError(t, err)
	require.Equal(t, clickup.SearchTasksBranchSearched, result.Branch)
	require.Len(t, result.Value.Tasks, 1)
	task := result.Value.Tasks[0]
	require.Equal(t, testTaskID, task.ID)
	require.Equal(t, clickup.TaskPriorityHigh, task.Priority)
	require.Equal(t, "high", task.PriorityName)
	require.Equal(t, []clickup.User{{ID: 183, Username: "Ada"}}, task.Assignees, "users never carry email addresses")
	require.Equal(t, []string{"escalated"}, task.Tags)
	require.Equal(t, testListID, task.ListID)
	require.Empty(t, task.CustomFields, "search results carry no custom fields")
	require.Equal(t, 2, result.Value.Page)
	require.Nil(t, result.Value.NextPage, "last_page ends the paging")

	request := provider.request(0)
	parsed, err := url.ParseRequestURI(request.path)
	require.NoError(t, err)
	require.Equal(t, "/team/"+testWorkspaceID+"/task", parsed.Path)
	require.Equal(t, url.Values{
		"page": {"2"}, "list_ids[]": {testListID, "901410000002"}, "project_ids[]": {"90140000077"}, "space_ids[]": {"90140000011"},
		"statuses[]": {"in progress", "blocked"}, "assignees[]": {"183", "184"}, "tags[]": {"escalated"},
		"date_updated_gt": {"1767225600123"}, "include_closed": {"true"}, "subtasks": {"true"}, "order_by": {"updated"}, "reverse": {"true"},
	}, parsed.Query())
}

func TestSearchTasksContinuesWithTheNextPage(t *testing.T) {
	fullPage := make([]json.RawMessage, 0, clickup.SearchTasksPageSize)
	for index := range clickup.SearchTasksPageSize {
		fullPage = append(fullPage, json.RawMessage(taskJSON(t, testTask{id: "task" + strconv.Itoa(index), name: "Task"})))
	}
	encodedPage, err := json.Marshal(fullPage)
	require.NoError(t, err)
	for _, test := range []struct {
		name     string
		body     string
		expected *int
	}{
		{name: "not last page", body: `{"tasks":[` + taskJSON(t, testTask{id: testTaskID, name: "A"}) + `],"last_page":false}`, expected: pointerTo(1)},
		{name: "full page without last_page", body: `{"tasks":` + string(encodedPage) + `}`, expected: pointerTo(1)},
		{name: "short page without last_page", body: `{"tasks":[]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.body)
			})
			result, err := sdkgo.RunQuery(newTestDexContext("page-"+test.name), newClickUpClient(t, provider.URL).SearchTasks(), clickupConnection,
				clickup.SearchTasksInput{WorkspaceID: testWorkspaceID})
			require.NoError(t, err)
			require.Equal(t, clickup.SearchTasksBranchSearched, result.Branch)
			require.Equal(t, test.expected, result.Value.NextPage)
			require.Equal(t, "/team/"+testWorkspaceID+"/task?page=0", provider.request(0).path)
		})
	}
}

func TestSearchTasksRejectsInvalidFiltersWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"tasks":[]}`)
	})
	for name, input := range map[string]clickup.SearchTasksInput{
		"workspace":     {WorkspaceID: "acme"},
		"list":          {WorkspaceID: testWorkspaceID, ListIDs: []string{"../team"}},
		"empty status":  {WorkspaceID: testWorkspaceID, Statuses: []string{""}},
		"assignee":      {WorkspaceID: testWorkspaceID, AssigneeIDs: []int64{0}},
		"order":         {WorkspaceID: testWorkspaceID, OrderBy: "priority"},
		"negative page": {WorkspaceID: testWorkspaceID, Page: -1},
		"tag":           {WorkspaceID: testWorkspaceID, Tags: []string{" padded"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newTestDexContext("invalid-"+name), newClickUpClient(t, provider.URL).SearchTasks(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.SearchTasksBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestSearchTasksInUnauthorizedWorkspaceIsNotFound(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, clickupError("OAUTH_027"))
	})
	result, err := sdkgo.RunQuery(newTestDexContext("unauthorized"), newClickUpClient(t, provider.URL).SearchTasks(), clickupConnection,
		clickup.SearchTasksInput{WorkspaceID: testWorkspaceID})
	require.NoError(t, err)
	require.Equal(t, clickup.SearchTasksBranchNotFound, result.Branch)
	require.Empty(t, result.Value.Tasks)
}

func pointerTo[T any](value T) *T {
	return &value
}
