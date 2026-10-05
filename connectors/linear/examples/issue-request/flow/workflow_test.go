// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuerequest

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
)

const (
	pickedTeamID = "2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4"
	inputTeamID  = "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d"
)

func TestBuildIssueRequestPrefersThePickedTeamAndDefaultsTheComment(t *testing.T) {
	request, err := BuildIssueRequest(Input{TeamID: " " + inputTeamID + " ", Title: " Fire drill ", AssigneeEmail: " alice@example.com "}, pickedTeamID)
	require.NoError(t, err)
	require.Equal(t, IssueRequest{TeamID: pickedTeamID, Title: "Fire drill", AssigneeEmail: "alice@example.com", Comment: DefaultComment}, request)

	request, err = BuildIssueRequest(Input{TeamID: inputTeamID, Title: "Fire drill", Comment: "Filed."}, "")
	require.NoError(t, err)
	require.Equal(t, inputTeamID, request.TeamID, "an unsaved picker uses the Start Flow input")
	require.Equal(t, "Filed.", request.Comment)
}

func TestBuildIssueRequestRejectsUnusableInput(t *testing.T) {
	for name, input := range map[string]Input{
		"no team":      {Title: "Fire drill"},
		"no title":     {TeamID: inputTeamID},
		"two lines":    {TeamID: inputTeamID, Title: "Fire\ndrill"},
		"not an email": {TeamID: inputTeamID, Title: "Fire drill", AssigneeEmail: "alice"},
	} {
		_, err := BuildIssueRequest(input, "")
		require.ErrorIs(t, err, errInvalidIssueRequest, name)
	}
}

func TestChooseOpenIssueTakesOnlyAnOpenIssueOfTheTeamWithTheExactTitle(t *testing.T) {
	request := IssueRequest{TeamID: pickedTeamID, Title: "Fire drill"}
	archivedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	issue := func(id string, title string, teamID string, stateType linear.WorkflowStateType) linear.IssueSummary {
		return linear.IssueSummary{ID: id, Title: title, Team: linear.TeamReference{ID: teamID}, State: linear.WorkflowStateReference{Type: stateType}}
	}
	archived := issue("archived", "Fire drill", pickedTeamID, linear.WorkflowStateTypeBacklog)
	archived.ArchivedAt = &archivedAt
	chosen, isFound := ChooseOpenIssue([]linear.IssueSummary{
		issue("lookalike", "Fire drill (2025)", pickedTeamID, linear.WorkflowStateTypeBacklog),
		issue("other-team", "Fire drill", inputTeamID, linear.WorkflowStateTypeBacklog),
		issue("done", "Fire drill", pickedTeamID, linear.WorkflowStateTypeCompleted),
		archived,
		issue("open", "Fire drill", pickedTeamID, linear.WorkflowStateTypeUnstarted),
	}, request)
	require.True(t, isFound)
	require.Equal(t, "open", chosen.ID)
	_, isFound = ChooseOpenIssue(nil, request)
	require.False(t, isFound)
}

func TestChooseTargetStateUsesTheNameOrTheFirstUnstartedState(t *testing.T) {
	states := linear.ListWorkflowStatesOutput{States: []linear.WorkflowState{
		{ID: "backlog", Name: "Backlog", Type: linear.WorkflowStateTypeBacklog},
		{ID: "todo", Name: "Todo", Type: linear.WorkflowStateTypeUnstarted},
		{ID: "doing", Name: "In Progress", Type: linear.WorkflowStateTypeStarted},
	}}
	state, isFound := ChooseTargetState(states, "in progress")
	require.True(t, isFound)
	require.Equal(t, "doing", state.ID)
	state, isFound = ChooseTargetState(states, "")
	require.True(t, isFound)
	require.Equal(t, "todo", state.ID)
	_, isFound = ChooseTargetState(states, "Shipped")
	require.False(t, isFound)
}

func TestMappersSearchOnlyOpenStatesAndCreateWithTheResolvedAssignee(t *testing.T) {
	request := IssueRequest{TeamID: pickedTeamID, Title: "Fire drill", Description: "Check panels.", AssigneeID: "user"}
	search := MapToSearchIssuesInput(request)
	require.Equal(t, pickedTeamID, search.Filter.TeamID)
	require.Equal(t, "Fire drill", search.Filter.Title)
	require.Equal(t, openStateTypes, search.Filter.StateTypes)
	require.Equal(t, linear.CreateIssueInput{TeamID: pickedTeamID, Title: "Fire drill", Description: "Check panels.", AssigneeID: "user"},
		MapToCreateIssueInput(request))
	require.Equal(t, linear.UpdateIssueInput{IssueID: "issue", StateID: "todo", AssigneeID: "user"},
		MapToUpdateIssueInput(IssueChange{IssueID: "issue", StateID: "todo", AssigneeID: "user"}))
}
