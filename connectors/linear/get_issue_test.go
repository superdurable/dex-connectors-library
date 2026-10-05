// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func fullIssueJSON(description string) map[string]any {
	issue := issueSummaryJSON(testIssueID, "ENG-7", "Fire panel wiring")
	for key, value := range map[string]any{
		"number": 7, "priorityLabel": "High", "estimate": 3, "branchName": "alice/eng-7-fire-panel-wiring", "trashed": false,
		"description": description, "startedAt": "2026-01-29T09:00:00.000Z", "completedAt": nil, "canceledAt": nil,
		"assignee": map[string]any{"id": testUserID, "name": "Alice Nguyen", "displayName": "alice"},
		"creator":  map[string]any{"id": testUserID, "name": "Alice Nguyen", "displayName": "alice"},
		"labels":   map[string]any{"nodes": []any{map[string]any{"id": testLabelID, "name": "Compliance", "color": "#eb5757"}}},
		"project":  map[string]any{"id": "a1b2c3d4-e5f6-4a7b-8c9d-0e1f2a3b4c5d", "name": "Facilities"},
		"cycle":    map[string]any{"id": "b2c3d4e5-f6a7-4b8c-9d0e-1f2a3b4c5d6e", "number": 12, "name": nil},
		"parent":   map[string]any{"id": "c3d4e5f6-a7b8-4c9d-8e1f-2a3b4c5d6e7f", "identifier": "ENG-1"},
	} {
		issue[key] = value
	}
	return issue
}

func TestGetIssueReadsTheDescriptionAndRelations(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{fullIssueJSON("Check **every** panel.")}}})
	})
	result, err := runQuery("get", newAPIKeyClient(t, provider.URL).GetIssue(), linear.GetIssueInput{IssueID: strings.ToUpper(testIssueID)})
	require.NoError(t, err)
	require.Equal(t, linear.GetIssueBranchFound, result.Branch)
	issue := result.Value
	require.Equal(t, testIssueID, issue.ID)
	require.Equal(t, 7, issue.Number)
	require.Equal(t, "High", issue.PriorityLabel)
	require.Equal(t, "Check **every** panel.", issue.Description)
	require.False(t, issue.IsDescriptionTruncated)
	require.Equal(t, &linear.UserReference{ID: testUserID, Name: "Alice Nguyen", DisplayName: "alice"}, issue.Assignee)
	require.Equal(t, []linear.IssueLabel{{ID: testLabelID, Name: "Compliance", Color: "#eb5757"}}, issue.Labels)
	require.Equal(t, "Facilities", issue.Project.Name)
	require.Equal(t, 12, issue.Cycle.Number)
	require.Equal(t, "ENG-1", issue.Parent.Identifier)
	require.Equal(t, time.Date(2026, 1, 29, 9, 0, 0, 0, time.UTC), *issue.StartedAt)
	require.Nil(t, issue.CompletedAt)
	require.Equal(t, testIssueID, result.Receipt.ProviderObjectID)
	require.Equal(t, map[string]any{"filter": map[string]any{"id": map[string]any{"eq": testIssueID}}}, provider.request(0).variables,
		"a UUID is compared in Linear's lower case")
}

func TestGetIssueCutsALongDescriptionOnACharacterBoundary(t *testing.T) {
	description := strings.Repeat("é", linear.MaxDescriptionCharacters+10)
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{fullIssueJSON(description)}}})
	})
	result, err := runQuery("long", newAPIKeyClient(t, provider.URL).GetIssue(), linear.GetIssueInput{IssueID: "ENG-7"})
	require.NoError(t, err)
	require.True(t, result.Value.IsDescriptionTruncated)
	require.Equal(t, linear.MaxDescriptionCharacters, utf8.RuneCountInString(result.Value.Description))
	require.True(t, utf8.ValidString(result.Value.Description))
}

func TestGetIssueSelectsNotFoundForAnEmptyListAndInvalidForAnotherIssue(t *testing.T) {
	answers := []any{
		map[string]any{"issues": map[string]any{"nodes": []any{}}},
		map[string]any{"issues": map[string]any{"nodes": []any{fullIssueJSON("")}}},
	}
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		writeData(t, response, answers[index])
	})
	client := newAPIKeyClient(t, provider.URL)
	missing, err := runQuery("missing", client.GetIssue(), linear.GetIssueInput{IssueID: "ENG-404"})
	require.NoError(t, err)
	require.Equal(t, linear.GetIssueBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	other, err := runQuery("other", client.GetIssue(), linear.GetIssueInput{IssueID: "OPS-7"})
	require.NoError(t, err)
	require.Equal(t, linear.GetIssueBranchInvalidResponse, other.Branch, "ENG-7 is not OPS-7")
}

func TestGetIssueRejectsAnUnusableIDWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	for _, issueID := range []string{"", "ENG", "ENG-0", "7", "ENG-7; drop"} {
		result, err := runQuery("bad-"+issueID, newAPIKeyClient(t, provider.URL).GetIssue(), linear.GetIssueInput{IssueID: issueID})
		require.NoError(t, err)
		require.Equal(t, linear.GetIssueBranchDefect, result.Branch, issueID)
	}
}
