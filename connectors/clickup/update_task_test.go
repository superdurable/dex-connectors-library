// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateTaskSendsAbsoluteChangesAndAssigneeSetMemberships(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: testTaskID, name: "Fix login", status: "in progress", assigneeIDs: []int64{183}}))
	})
	dueAt := time.UnixMilli(1767484800000)
	name := "Fix login"
	result, err := sdkgo.RunMutation(newTestDexContext("update"), newClickUpClient(t, provider.URL).UpdateTask(), clickupConnection, clickup.UpdateTaskInput{
		TaskID: testTaskID, Name: &name, Status: "in progress", Priority: clickup.TaskPriorityUrgent, DueAt: &dueAt,
		AddAssigneeIDs: []int64{183}, RemoveAssigneeIDs: []int64{184},
	})
	require.NoError(t, err)
	require.Equal(t, clickup.UpdateTaskBranchUpdated, result.Branch)
	require.Equal(t, "in progress", result.Value.Task.Status.Name)
	request := provider.request(0)
	require.Equal(t, http.MethodPut, request.method)
	require.Equal(t, "/task/"+testTaskID, request.path)
	require.JSONEq(t, `{"name":"Fix login","status":"in progress","priority":1,"due_date":1767484800000,"due_date_time":true,
		"assignees":{"add":[183],"rem":[184]}}`, request.body)
}

func TestUpdateTaskDescriptionChanges(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: testTaskID, name: "Fix login"}))
	})
	client := newClickUpClient(t, provider.URL)
	description, cleared := "New **steps**", ""
	_, err := sdkgo.RunMutation(newTestDexContext("describe"), client.UpdateTask(), clickupConnection, clickup.UpdateTaskInput{TaskID: testTaskID, MarkdownDescription: &description})
	require.NoError(t, err)
	require.JSONEq(t, `{"markdown_content":"New **steps**"}`, provider.request(0).body)
	_, err = sdkgo.RunMutation(newTestDexContext("clear"), client.UpdateTask(), clickupConnection, clickup.UpdateTaskInput{TaskID: testTaskID, MarkdownDescription: &cleared})
	require.NoError(t, err)
	require.JSONEq(t, `{"description":" "}`, provider.request(1).body, "ClickUp clears a description with a single space")
	_, err = sdkgo.RunMutation(newTestDexContext("assign"), client.UpdateTask(), clickupConnection, clickup.UpdateTaskInput{TaskID: testTaskID, AddAssigneeIDs: []int64{183}})
	require.NoError(t, err)
	require.JSONEq(t, `{"assignees":{"add":[183],"rem":[]}}`, provider.request(2).body, "add and rem are both present")
}

func TestUpdateTaskRetriesEveryAmbiguousOutcome(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			hijackAndClose(t, response)
			return
		}
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: testTaskID, name: "Fix login", status: "blocked"}))
	})
	client := newClickUpClient(t, provider.URL)
	input := clickup.UpdateTaskInput{TaskID: testTaskID, Status: "blocked"}
	_, err := sdkgo.RunMutation(newTestDexContext("update-lost"), client.UpdateTask(), clickupConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a repeated PUT leaves the same task, so a lost response retries")
	result, err := sdkgo.RunMutation(newTestDexContext("update-lost"), client.UpdateTask(), clickupConnection, input)
	require.NoError(t, err)
	require.Equal(t, clickup.UpdateTaskBranchUpdated, result.Branch)
	require.Equal(t, provider.request(0).body, provider.request(1).body)
}

func TestUpdateTaskRejectsAStatusOutsideTheListWorkflow(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, clickupError("ITEM_169"))
	})
	result, err := sdkgo.RunMutation(newTestDexContext("update-status"), newClickUpClient(t, provider.URL).UpdateTask(), clickupConnection,
		clickup.UpdateTaskInput{TaskID: testTaskID, Status: "Done!"})
	require.NoError(t, err)
	require.Equal(t, clickup.UpdateTaskBranchProviderRejected, result.Branch)
	require.Equal(t, "ClickUp rejected the request (HTTP 400) [ITEM_169]", result.Failure.Message)
}

func TestUpdateTaskValidatesChangesWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	blank := " "
	for name, input := range map[string]clickup.UpdateTaskInput{
		"no change":          {TaskID: testTaskID},
		"blank name":         {TaskID: testTaskID, Name: &blank},
		"add and remove":     {TaskID: testTaskID, AddAssigneeIDs: []int64{183}, RemoveAssigneeIDs: []int64{183}},
		"priority":           {TaskID: testTaskID, Priority: -1},
		"task":               {TaskID: "", Status: "open"},
		"negative assignees": {TaskID: testTaskID, RemoveAssigneeIDs: []int64{-2}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newTestDexContext("update-invalid-"+name), newClickUpClient(t, provider.URL).UpdateTask(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.UpdateTaskBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}
