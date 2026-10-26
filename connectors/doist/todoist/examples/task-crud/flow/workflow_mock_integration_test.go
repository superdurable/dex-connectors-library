//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskcrud

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist"
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist/todoistmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTaskLifecycleUsesMockPagesAndVerifiesDeletionWithRealDex(t *testing.T) {
	mock := todoistmock.New(t, ConnectionName)
	mock.GetTask().Respond(
		todoistmock.GetTaskCreatedTask(),
		todoistmock.GetTaskFound(todoist.Task{ID: "created-task", ProjectID: "inbox-project", Content: "Original title - updated", Description: "Original description"}, nil),
		todoistmock.GetTaskDeletedTaskMissing(),
	)
	flow, harness := newCRUDHarnessWithConnection(t, mock.Connection())
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := Request{Mode: "lifecycle", Content: "Original title", Description: "Original description"}
	result := harness.runFlow(t, ctx, flow, "todoist-mock-"+strconv.FormatInt(time.Now().UnixNano(), 10), request)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, "deletedAndVerified", outcome.Status)
	require.True(t, outcome.MatchesRequestedFields)
	require.Equal(t, request.Content+" - updated", outcome.Updated.Content)
	require.Equal(t, outcome.Created.ID, outcome.Updated.ID)
	require.Equal(t, "deleted", outcome.DeleteBranch)

	createCalls := mock.CreateTask().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, todoist.CreateTaskInput{Content: request.Content, Description: request.Description}, createCalls[0].Input)
	getCalls := mock.GetTask().Calls()
	require.Len(t, getCalls, 3)
	for _, call := range getCalls {
		require.Equal(t, todoist.GetTaskInput{TaskID: outcome.Created.ID}, call.Input)
	}
	updateCalls := mock.UpdateTask().Calls()
	require.Len(t, updateCalls, 1)
	require.Equal(t, todoist.UpdateTaskInput{TaskID: outcome.Created.ID, Content: &outcome.Updated.Content}, updateCalls[0].Input)
	listCalls := mock.ListTasks().Calls()
	require.Len(t, listCalls, 3)
	require.Equal(t, []string{"", "inbox-page-2", "inbox-page-3"}, []string{listCalls[0].Input.Cursor, listCalls[1].Input.Cursor, listCalls[2].Input.Cursor})
	for _, call := range listCalls {
		require.Equal(t, todoist.ListTasksInput{ProjectID: outcome.Created.ProjectID, Limit: 200, Cursor: call.Input.Cursor}, call.Input)
	}
	deleteCalls := mock.DeleteTask().Calls()
	require.Len(t, deleteCalls, 1)
	require.Equal(t, todoist.DeleteTaskInput{TaskID: outcome.Created.ID}, deleteCalls[0].Input)
}
