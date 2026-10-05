// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateIssueSendsAbsoluteValuesAndLabelSetOperations(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		issue := issueSummaryJSON(testIssueID, "ENG-7", "Fire panel wiring")
		issue["state"] = map[string]any{"id": testOtherStateID, "name": "In Progress", "type": "started"}
		writeData(t, response, map[string]any{"issueUpdate": map[string]any{"success": true, "issue": issue}})
	})
	priority := 1
	result, err := runMutation("update", newAPIKeyClient(t, provider.URL).UpdateIssue(), linear.UpdateIssueInput{
		IssueID: "eng-7", StateID: testOtherStateID, AssigneeID: testUserID, AddedLabelIDs: []string{testLabelID},
		RemovedLabelIDs: []string{testStateID}, Priority: &priority, ClearsDueDate: true, Title: " Fire panel wiring ",
	})
	require.NoError(t, err)
	require.Equal(t, linear.UpdateIssueBranchUpdated, result.Branch)
	require.Equal(t, "ENG-7", result.Value.IssueID)
	require.Equal(t, linear.WorkflowStateTypeStarted, result.Value.Issue.State.Type)
	require.Equal(t, testIssueID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, "LinearUpdateIssue", request.operationName)
	require.Equal(t, "ENG-7", request.variables["id"])
	require.Equal(t, map[string]any{
		"stateId": testOtherStateID, "assigneeId": testUserID, "addedLabelIds": []any{testLabelID}, "removedLabelIds": []any{testStateID},
		"priority": float64(1), "dueDate": nil, "title": "Fire panel wiring",
	}, request.variables["input"])
}

func TestUpdateIssueUnassignsWithNull(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		issue := issueSummaryJSON(testIssueID, "ENG-7", "Fire panel wiring")
		issue["assignee"] = nil
		writeData(t, response, map[string]any{"issueUpdate": map[string]any{"success": true, "issue": issue}})
	})
	result, err := runMutation("unassign", newAPIKeyClient(t, provider.URL).UpdateIssue(), linear.UpdateIssueInput{IssueID: testIssueID, Unassigns: true})
	require.NoError(t, err)
	require.Equal(t, linear.UpdateIssueBranchUpdated, result.Branch)
	require.Empty(t, result.Value.Issue.AssigneeID)
	input := provider.request(0).variables["input"].(map[string]any)
	require.Contains(t, input, "assigneeId")
	require.Nil(t, input["assigneeId"])
}

func TestUpdateIssueReadsTheIssueAfterAnInputRejection(t *testing.T) {
	for name, testCase := range map[string]struct {
		readBack []any
		branch   sdkgo.BranchID
		kind     sdkgo.FailureKind
	}{
		"missing issue":         {readBack: []any{}, branch: linear.UpdateIssueBranchNotFound, kind: sdkgo.FailureNotFound},
		"state of another team": {readBack: []any{issueSummaryJSON(testIssueID, "ENG-7", "x")}, branch: linear.UpdateIssueBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
				if request.operationName == "LinearUpdateIssue" {
					writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
					return
				}
				writeData(t, response, map[string]any{"issues": map[string]any{"nodes": testCase.readBack}})
			})
			result, err := runMutation("rejected-"+name, newAPIKeyClient(t, provider.URL).UpdateIssue(), linear.UpdateIssueInput{IssueID: testIssueID, StateID: testStateID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.Equal(t, []string{"LinearUpdateIssue", "LinearReadIssueSummary"}, provider.operationNames())
		})
	}
}

func TestUpdateIssueRetriesAvailabilityFailuresAndReportsUnusableAnswers(t *testing.T) {
	unavailable := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, 500, linearError("INTERNAL_SERVER_ERROR", "internal error"))
	})
	_, err := runMutation("unavailable", newAPIKeyClient(t, unavailable.URL).UpdateIssue(), linear.UpdateIssueInput{IssueID: testIssueID, StateID: testStateID})
	requireRetry(t, err, sdkgo.FailureAvailability, 0)
	require.Equal(t, 1, unavailable.requestCount(), "an absolute update is retried, not read back")

	unusable := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"issueUpdate": map[string]any{"success": true, "issue": map[string]any{"id": "x"}}})
	})
	result, err := runMutation("unusable", newAPIKeyClient(t, unusable.URL).UpdateIssue(), linear.UpdateIssueInput{IssueID: testIssueID, StateID: testStateID})
	require.NoError(t, err)
	require.Equal(t, linear.UpdateIssueBranchInvalidResponse, result.Branch)
}

func TestUpdateIssueRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	for name, input := range map[string]linear.UpdateIssueInput{
		"no change":            {IssueID: testIssueID},
		"assign and unassign":  {IssueID: testIssueID, AssigneeID: testUserID, Unassigns: true},
		"due date and clear":   {IssueID: testIssueID, DueDate: "2026-02-18", ClearsDueDate: true},
		"label added and gone": {IssueID: testIssueID, AddedLabelIDs: []string{testLabelID}, RemovedLabelIDs: []string{testLabelID}},
		"state name":           {IssueID: testIssueID, StateID: "In Progress"},
		"no issue":             {StateID: testStateID},
	} {
		result, err := runMutation("invalid-"+name, newAPIKeyClient(t, provider.URL).UpdateIssue(), input)
		require.NoError(t, err, name)
		require.Equal(t, linear.UpdateIssueBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
