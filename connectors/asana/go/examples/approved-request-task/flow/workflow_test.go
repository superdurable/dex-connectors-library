// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequesttask

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBuildTaskRequestPrefersThePickedProjectAndWritesTheApprovalComment(t *testing.T) {
	input := Input{
		RequestID: " REQ-1042 ", Title: " Replace badge reader ", Details: "Door 4 is offline.", ApprovedBy: " Grace Hopper ",
		ApprovalNote: " Budget approved. ", AssigneeID: " ada@example.com ", DueOn: "2026-10-15", ProjectID: "111", SectionID: "112",
	}
	request, err := BuildTaskRequest(ProjectSelection{ProjectID: "1201000000000001", SectionID: "1201000000000101"}, input)
	require.NoError(t, err)
	require.Equal(t, TaskRequest{
		RequestID: "REQ-1042", TaskName: "[REQ-1042] Replace badge reader", Notes: "Door 4 is offline.",
		ProjectID: "1201000000000001", SectionID: "1201000000000101", AssigneeID: "ada@example.com", DueOn: "2026-10-15",
		Comment: "Approved by Grace Hopper.\n\nBudget approved.",
	}, request)

	request, err = BuildTaskRequest(ProjectSelection{}, input)
	require.NoError(t, err)
	require.Equal(t, "111", request.ProjectID, "an unsaved picker uses the Start Flow projectId")
	require.Equal(t, "112", request.SectionID)

	for _, invalid := range []Input{
		{Title: "Replace badge reader", ApprovedBy: "Grace", AssigneeID: "me", ProjectID: "111"},
		{RequestID: "REQ 1042", Title: "Replace badge reader", ApprovedBy: "Grace", AssigneeID: "me", ProjectID: "111"},
		{RequestID: "REQ-1042", Title: "one\ntwo", ApprovedBy: "Grace", AssigneeID: "me", ProjectID: "111"},
		{RequestID: "REQ-1042", Title: "Replace badge reader", AssigneeID: "me", ProjectID: "111"},
		{RequestID: "REQ-1042", Title: "Replace badge reader", ApprovedBy: "Grace", AssigneeID: "me"},
		{RequestID: "REQ-1042", Title: "Replace badge reader", ApprovedBy: "Grace", AssigneeID: "me", ProjectID: "Facilities"},
		{RequestID: "REQ-1042", Title: "Replace badge reader", ApprovedBy: "Grace", ProjectID: "111"},
	} {
		_, err := BuildTaskRequest(ProjectSelection{}, invalid)
		require.Error(t, err, "%+v", invalid)
	}
}

func TestFindRequestTaskMatchesOnlyTheBracketedRequestIDOfAnOpenTask(t *testing.T) {
	tasks := []asana.Task{
		{ID: "1", Name: "[REQ-10420] Replace badge reader"},
		{ID: "2", Name: "Follow-up on REQ-1042"},
		{ID: "3", Name: "[REQ-1042] Replace badge reader", IsCompleted: true},
		{ID: "4", Name: " [REQ-1042] Replace badge reader (reopened)"},
	}
	task, isFound := FindRequestTask(tasks, "REQ-1042")
	require.True(t, isFound)
	require.Equal(t, "4", task.ID)
	_, isFound = FindRequestTask(tasks[:3], "REQ-1042")
	require.False(t, isFound)
}

func TestMappersBuildTheConnectorInputs(t *testing.T) {
	request := TaskRequest{
		RequestID: "REQ-1042", TaskName: "[REQ-1042] Replace badge reader", Notes: "Door 4.", ProjectID: "11", SectionID: "12",
		AssigneeID: "me", DueOn: "2026-10-15", Comment: "Approved by Grace.",
	}
	require.Equal(t, asana.ListTasksInput{ProjectID: "11", IsIncompleteOnly: true, PageSize: 100, Offset: "next"},
		MapToListTasksInput(OpenTaskPage{ProjectID: "11", Offset: "next"}))
	require.Equal(t, asana.CreateTaskInput{
		Name: "[REQ-1042] Replace badge reader", Notes: "Door 4.", ProjectID: "11", SectionID: "12", DueOn: "2026-10-15",
	}, MapToCreateTaskInput(request))
	assigneeID, dueOn := "me", "2026-10-15"
	require.Equal(t, asana.UpdateTaskInput{TaskID: "13", AssigneeID: &assigneeID, DueOn: &dueOn, SectionID: "12"},
		MapToUpdateTaskInput(TaskAssignment{TaskID: "13", AssigneeID: "me", DueOn: "2026-10-15", SectionID: "12"}))
	require.Nil(t, MapToUpdateTaskInput(TaskAssignment{TaskID: "13", AssigneeID: "me"}).DueOn, "a blank due date leaves the task's own")
	require.Equal(t, asana.AddCommentInput{TaskID: "13", Text: "Approved by Grace."}, MapToAddCommentInput(CommentRequest{TaskID: "13", Text: "Approved by Grace."}))
}

func TestReportedTaskIDsAreValidated(t *testing.T) {
	require.True(t, isTaskGID("1204567890123456"))
	for _, value := range []string{"", "REQ-1042", "https://app.asana.com/0/1/2", "12 34"} {
		require.False(t, isTaskGID(value), value)
	}
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	client, err := asana.New(asana.Config{}, sdkgo.StaticCredentialProvider[asana.Credentials]{})
	require.NoError(t, err)
	connection, err := asana.NewConnection(client, sdkgo.ConnectionRef{Provider: "asana", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection, ProjectSelection{ProjectID: "1201000000000001"})
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 5)
}
