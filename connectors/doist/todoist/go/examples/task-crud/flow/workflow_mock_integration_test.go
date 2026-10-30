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
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist/go"
	"github.com/superdurable/dex-connectors-library/connectors/doist/todoist/go/todoistmock"
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
	require.Equal(t, "created-task", outcome.Created.ID)
	require.Equal(t, "inbox-project", outcome.Created.ProjectID)
	require.Equal(t, "Original title - updated", outcome.Updated.Content)
	require.Equal(t, "created-task", outcome.Updated.ID)
	require.Equal(t, "deleted", outcome.DeleteBranch)

	createCalls := mock.CreateTask().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, todoist.CreateTaskInput{Content: "Original title", Description: "Original description"}, createCalls[0].Input)
	getCalls := mock.GetTask().Calls()
	require.Len(t, getCalls, 3)
	for _, call := range getCalls {
		require.Equal(t, todoist.GetTaskInput{TaskID: "created-task"}, call.Input)
	}
	updateCalls := mock.UpdateTask().Calls()
	require.Len(t, updateCalls, 1)
	updatedTitle := "Original title - updated"
	require.Equal(t, todoist.UpdateTaskInput{TaskID: "created-task", Content: &updatedTitle}, updateCalls[0].Input)
	listCalls := mock.ListTasks().Calls()
	require.Len(t, listCalls, 3)
	for index, call := range listCalls {
		require.Equal(t, todoist.ListTasksInput{ProjectID: "inbox-project", Limit: 200, Cursor: []string{"", "inbox-page-2", "inbox-page-3"}[index]}, call.Input)
	}
	deleteCalls := mock.DeleteTask().Calls()
	require.Len(t, deleteCalls, 1)
	require.Equal(t, todoist.DeleteTaskInput{TaskID: "created-task"}, deleteCalls[0].Input)
}
