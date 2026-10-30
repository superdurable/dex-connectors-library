// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestBuildTriageRequestPrefersThePickedProjectAndDefaultsTheIssueType(t *testing.T) {
	request, err := BuildTriageRequest(" OPS ", Input{
		Summary: "  Fire panel wiring ", ProjectKey: "FAC", TriageComment: " Paging facilities. ", DestinationStatusName: " In Progress ",
	})
	require.NoError(t, err)
	require.Equal(t, TriageRequest{
		ProjectKey: "OPS", Summary: "Fire panel wiring", IssueTypeName: DefaultIssueTypeName,
		TriageComment: "Paging facilities.", DestinationStatusName: "In Progress",
	}, request)

	request, err = BuildTriageRequest("", Input{Summary: "Fire panel wiring", ProjectKey: "FAC", IssueTypeName: "Bug"})
	require.NoError(t, err)
	require.Equal(t, "FAC", request.ProjectKey)
	require.Equal(t, "Bug", request.IssueTypeName)

	for _, input := range []Input{{Summary: "Fire panel wiring"}, {ProjectKey: "OPS"}, {ProjectKey: "OPS", Summary: "one\ntwo"}} {
		_, err := BuildTriageRequest("", input)
		require.Error(t, err, "%+v", input)
	}
}

func TestFindSameSummaryIssueRejectsNearDuplicatesAndDoneIssues(t *testing.T) {
	issues := []jira.Issue{
		{Key: "OPS-1", Summary: "Fire panel wiring (2025)", Status: jira.IssueStatus{CategoryKey: jira.StatusCategoryToDo}},
		{Key: "OPS-2", Summary: "fire panel wiring", Status: jira.IssueStatus{CategoryKey: jira.StatusCategoryDone}},
		{Key: "OPS-3", Summary: " FIRE PANEL WIRING ", Status: jira.IssueStatus{CategoryKey: jira.StatusCategoryInProgress}},
	}
	issue, isFound := FindSameSummaryIssue(issues, "Fire panel wiring")
	require.True(t, isFound)
	require.Equal(t, "OPS-3", issue.Key)
	_, isFound = FindSameSummaryIssue(issues[:2], "Fire panel wiring")
	require.False(t, isFound)
}

func TestMappersBuildTheConnectorInputs(t *testing.T) {
	request := TriageRequest{
		ProjectKey: "OPS", Summary: "Fire panel wiring", Description: "Panel B trips.", IssueTypeName: "Task",
		Labels: []string{"incident"}, AssigneeAccountID: "5b10ac8d82e05b22cc7d4ef5",
	}
	search := MapToSearchIssuesInput(request)
	jql, err := search.Filter.JQL()
	require.NoError(t, err)
	require.Equal(t, `project in ("OPS") AND statusCategory in (2, 4) AND summary ~ "\"Fire panel wiring\"" ORDER BY created DESC`, jql)
	require.Equal(t, jira.CreateIssueInput{
		ProjectKey: "OPS", IssueTypeName: "Task", Summary: "Fire panel wiring", Description: "Panel B trips.",
		Labels: []string{"incident"}, AssigneeAccountID: "5b10ac8d82e05b22cc7d4ef5",
	}, MapToCreateIssueInput(request))
	require.Equal(t, jira.TransitionIssueInput{IssueIDOrKey: "OPS-7", DestinationStatusName: "Done"},
		MapToTransitionIssueInput(StatusChangeRequest{IssueKey: "OPS-7", DestinationStatusName: "Done"}))
}

func TestReportedIssueKeysAreValidated(t *testing.T) {
	for _, key := range []string{"OPS-442", "A1_B-9"} {
		require.True(t, isIssueKey(key), key)
	}
	for _, key := range []string{"", "OPS", "OPS-0", "ops-1", "1OPS-1", "OPS-1-2", "OPS-12345678901234567890"} {
		require.False(t, isIssueKey(key), key)
	}
}

func TestFlowRegistersEveryStepAndRPC(t *testing.T) {
	client, err := jira.New(jira.Config{CloudID: "11223344-a1b2-4b33-8c44-def123456789"}, sdkgo.StaticCredentialProvider[jira.Credentials]{})
	require.NoError(t, err)
	connection, err := jira.NewConnection(client, sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName})
	require.NoError(t, err)
	flow := NewFlow(connection, ProjectSelection{ProjectKey: "OPS"})
	_, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	require.Len(t, flow.GetRPCs(), 5)
}
