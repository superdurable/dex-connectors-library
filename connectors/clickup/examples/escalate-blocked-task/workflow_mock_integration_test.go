//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/connectors/clickup/clickupmock"
	escalateblocked "github.com/superdurable/dex-connectors-library/connectors/clickup/examples/escalate-blocked-task/flow"
)

func TestBlockedTaskUsesMockDefaultsWithRealDex(t *testing.T) {
	mock := clickupmock.New(t, escalateblocked.ConnectionName)
	harness := newEscalationHarnessWithConnection(t, mock.Connection(), exampleSettings)
	outcome := harness.runEscalation(t, "mock-default", "86b2x1")

	require.Equal(t, escalateblocked.OutcomeEscalated, outcome.Action)
	require.False(t, outcome.WasExistingEscalationFound, "the Flow ignores other task IDs across three search pages")
	require.NotEqual(t, outcome.TaskID, outcome.EscalationTaskID)
	require.False(t, outcome.WasCreateAlreadyApplied)
	require.False(t, outcome.WasCommentAlreadyApplied)
	reads := mock.GetTask().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, clickup.GetTaskInput{TaskID: "86b2x1"}, reads[0].Input)
	managers := mock.FindMemberByEmail().Calls()
	require.Len(t, managers, 1)
	require.Equal(t, clickup.FindMemberByEmailInput{WorkspaceID: "9014123456", Email: "manager@acme.example.com"}, managers[0].Input)
	searches := mock.SearchTasks().Calls()
	require.Len(t, searches, 3)
	for index, search := range searches {
		require.Equal(t, clickup.SearchTasksInput{
			WorkspaceID: "9014123456", ListIDs: []string{"901410000099"}, Tags: []string{"escalation"},
			IsClosedIncluded: true, AreSubtasksIncluded: true, Page: index,
		}, search.Input)
	}
	creates := mock.CreateTask().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, clickup.CreateTaskInput{
		ListID: "901410000099", Name: "Escalation: Fix login for SSO customers [86b2x1]",
		MarkdownDescription: "[The blocked task](https://app.clickup.com/t/86b2x1) moved to the **blocked** status and needs a decision.",
		Priority:            clickup.TaskPriorityUrgent, AssigneeIDs: []int64{183}, Tags: []string{"escalation"},
	}, creates[0].Input)
	require.Equal(t, outcome.ManagerID, creates[0].Input.AssigneeIDs[0])
	tags := mock.UpdateTaskTags().Calls()
	require.Len(t, tags, 1)
	require.Equal(t, clickup.UpdateTaskTagsInput{TaskID: "86b2x1", AddTags: []string{"escalated"}}, tags[0].Input)
	updates := mock.UpdateTask().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, clickup.UpdateTaskInput{TaskID: "86b2x1", Priority: clickup.TaskPriorityUrgent, AddAssigneeIDs: []int64{183}}, updates[0].Input)
	comments := mock.AddComment().Calls()
	require.Len(t, comments, 1)
	require.Equal(t, clickup.AddCommentInput{
		TaskID: "86b2x1", Text: "Escalated to Ada Lovelace: https://app.clickup.com/t/86b2x9",
	}, comments[0].Input)
}
