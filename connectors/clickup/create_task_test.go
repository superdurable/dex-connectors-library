// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createdTaskName = "Escalation: Fix login [86b2x4k00]"

func validCreateTaskInput() clickup.CreateTaskInput {
	dueAt := time.UnixMilli(1767484800000)
	return clickup.CreateTaskInput{
		ListID: testListID, Name: createdTaskName, MarkdownDescription: "Blocked task: **Fix login**", Status: "to do",
		Priority: clickup.TaskPriorityUrgent, DueAt: &dueAt, AssigneeIDs: []int64{183}, Tags: []string{"escalation"},
		CustomFields: []clickup.CustomFieldInput{{ID: "d2ab17e0-41d3-4d28-bdbb-f3f0f2c62f8b", Value: json.RawMessage(`"f8249998-9c0a-4b1d-a8b7-67a072aca05a"`)}},
	}
}

func TestCreateTaskSendsTheTaskAfterRecordingACheckpoint(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName, assigneeIDs: []int64{183}, tags: []string{"escalation"}}))
	})
	dexContext := newTestDexContext("create")
	result, err := sdkgo.RunMutation(dexContext, newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchCreated, result.Branch)
	require.Equal(t, "86b2x9z11", result.Value.Task.ID)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "86b2x9z11", result.Receipt.ProviderObjectID)
	require.True(t, dexContext.hasHeartbeat(), "the dispatch checkpoint stays recorded")
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/list/"+testListID+"/task", request.path)
	require.JSONEq(t, `{"name":"`+createdTaskName+`","markdown_content":"Blocked task: **Fix login**","status":"to do","priority":1,
		"due_date":1767484800000,"due_date_time":true,"assignees":[183],"tags":["escalation"],
		"custom_fields":[{"id":"d2ab17e0-41d3-4d28-bdbb-f3f0f2c62f8b","value":"f8249998-9c0a-4b1d-a8b7-67a072aca05a"}]}`, request.body)
}

func TestUnconfirmedCreateIsReconciledFromTheListAndNeverResent(t *testing.T) {
	dispatchedAt := time.UnixMilli(1767225600000)
	for _, test := range []struct {
		name           string
		tasks          []testTask
		expectedBranch sdkgo.BranchID
		expectedTaskID string
	}{
		{name: "one matching task", tasks: []testTask{
			{id: "86b2x9z11", name: createdTaskName, createdAt: dispatchedAt.Add(time.Second)},
			{id: "86b2x9z12", name: "Another task", createdAt: dispatchedAt.Add(time.Second)},
		}, expectedBranch: clickup.CreateTaskBranchCreated, expectedTaskID: "86b2x9z11"},
		{name: "no matching task", tasks: []testTask{{id: "86b2x9z12", name: "Another task", createdAt: dispatchedAt}}, expectedBranch: clickup.CreateTaskBranchUncertain},
		{name: "a subtask of another parent", tasks: []testTask{{id: "86b2x9z13", name: createdTaskName, parent: "86b2x4k99", createdAt: dispatchedAt}},
			expectedBranch: clickup.CreateTaskBranchUncertain},
		{name: "two matching tasks", tasks: []testTask{
			{id: "86b2x9z11", name: createdTaskName, createdAt: dispatchedAt}, {id: "86b2x9z14", name: createdTaskName, createdAt: dispatchedAt},
		}, expectedBranch: clickup.CreateTaskBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
				if index == 0 {
					hijackAndClose(t, response)
					return
				}
				tasks := []any{}
				for _, task := range test.tasks {
					tasks = append(tasks, taskMap(task))
				}
				encoded, err := json.Marshal(map[string]any{"tasks": tasks, "last_page": true})
				require.NoError(t, err)
				writeJSON(t, response, http.StatusOK, string(encoded))
			})
			client := newClickUpClient(t, provider.URL, clickup.WithClock(func() time.Time { return dispatchedAt }))
			dexContext := newTestDexContext("create-" + test.name)
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a lost response retries with the checkpoint held")
			require.True(t, dexContext.hasHeartbeat())

			result, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedTaskID, result.Value.Task.ID)
			require.Equal(t, test.expectedBranch == clickup.CreateTaskBranchCreated, result.Value.WasAlreadyApplied)
			require.Equal(t, 2, provider.requestCount(), "the retried attempt reads the List and sends nothing")
			reconcile := provider.request(1)
			parsed, err := url.ParseRequestURI(reconcile.path)
			require.NoError(t, err)
			require.Equal(t, http.MethodGet, reconcile.method)
			require.Equal(t, "/list/"+testListID+"/task", parsed.Path)
			require.Equal(t, url.Values{
				"date_created_gt": {strconv.FormatInt(dispatchedAt.Add(-5*time.Minute).UnixMilli(), 10)},
				"include_closed":  {"true"}, "subtasks": {"true"}, "page": {"0"},
			}, parsed.Query())
		})
	}
}

func TestCreateTaskResendsOnlyWhenClickUpCannotHaveAppliedIt(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("X-RateLimit-Reset", "1")
			writeJSON(t, response, http.StatusTooManyRequests, clickupError("RATE_001"))
			return
		}
		writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z11", name: createdTaskName}))
	})
	dexContext := newTestDexContext("create-rate-limited")
	client := newClickUpClient(t, provider.URL)
	_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.False(t, dexContext.hasHeartbeat(), "a 429 releases the checkpoint")
	result, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchCreated, result.Branch)
	require.Equal(t, http.MethodPost, provider.request(1).method, "the create is sent again")

	unreachable := newClickUpClient(t, "http://"+closedLoopbackAddress(t))
	notSentContext := newTestDexContext("create-unreachable")
	_, err = sdkgo.RunMutation(notSentContext, unreachable.CreateTask(), clickupConnection, validCreateTaskInput())
	require.ErrorAs(t, err, &retry)
	require.Equal(t, "ClickUp could not be reached; no request was sent", retry.Failure.Message)
	require.False(t, notSentContext.hasHeartbeat(), "a connection that never opened releases the checkpoint")
}

func TestCreateTaskRejectionAndCheckpointFailure(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, clickupError("ITEM_015"))
	})
	result, err := sdkgo.RunMutation(newTestDexContext("create-rejected"), newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchProviderRejected, result.Branch)
	require.Equal(t, "ClickUp rejected the request (HTTP 400) [ITEM_015]", result.Failure.Message)
	require.Equal(t, createdTaskName, result.Value.Name)
	require.Empty(t, result.Value.Task.ID)

	failingContext := newTestDexContext("create-no-checkpoint")
	failingContext.recordErr = errors.New("dex unavailable")
	_, err = sdkgo.RunMutation(failingContext, newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 1, provider.requestCount(), "nothing is sent without a checkpoint")
}

func TestCreateTaskValidatesTheTaskWithoutARequest(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	for name, change := range map[string]func(*clickup.CreateTaskInput){
		"list":               func(input *clickup.CreateTaskInput) { input.ListID = "Escalations" },
		"blank name":         func(input *clickup.CreateTaskInput) { input.Name = "  " },
		"priority":           func(input *clickup.CreateTaskInput) { input.Priority = 5 },
		"duplicate assignee": func(input *clickup.CreateTaskInput) { input.AssigneeIDs = []int64{183, 183} },
		"custom field value": func(input *clickup.CreateTaskInput) { input.CustomFields[0].Value = json.RawMessage(`{`) },
		"parent":             func(input *clickup.CreateTaskInput) { input.ParentTaskID = "a/b" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validCreateTaskInput()
			change(&input)
			result, err := sdkgo.RunMutation(newTestDexContext("create-invalid-"+name), newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, clickup.CreateTaskBranchDefect, result.Branch)
		})
	}
	require.Zero(t, provider.requestCount())
}

// hijackAndClose drops the connection after ClickUp received the request, so its outcome is unknown.
func hijackAndClose(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// closedLoopbackAddress returns a loopback address with no listener, so a dial fails before sending.
func closedLoopbackAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}
