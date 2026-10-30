// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"fmt"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validCreateIssueInput() linear.CreateIssueInput {
	priority := 2
	return linear.CreateIssueInput{
		TeamID: testTeamID, Title: "  Monthly Fire Drill Checklist - February  ", Description: "Check every panel.\n\n- Floor 1",
		StateID: testStateID, AssigneeID: testUserID, LabelIDs: []string{testLabelID}, Priority: &priority, DueDate: "2026-02-18",
		ParentID: "eng-1",
	}
}

// createdIssueAnswer returns the issue under the ID the request supplied, as Linear stores a client ID.
func createdIssueAnswer(request recordedRequest) map[string]any {
	input := request.variables["input"].(map[string]any)
	return map[string]any{"issueCreate": map[string]any{"success": true, "issue": issueSummaryJSON(fmt.Sprint(input["id"]), "ENG-8", fmt.Sprint(input["title"]))}}
}

func TestCreateIssueSendsAClientSuppliedUUIDThatEveryAttemptRepeats(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		writeData(t, response, createdIssueAnswer(request))
	})
	client := newAPIKeyClient(t, provider.URL)
	first, err := runMutation("create", client.CreateIssue(), validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, linear.CreateIssueBranchCreated, first.Branch)
	input := provider.request(0).variables["input"].(map[string]any)
	issueID := fmt.Sprint(input["id"])
	require.Regexp(t, uuidV4Pattern, issueID, "Linear documents a UUID v4 for client IDs")
	require.Equal(t, map[string]any{
		"id": issueID, "teamId": testTeamID, "title": "Monthly Fire Drill Checklist - February", "description": "Check every panel.\n\n- Floor 1",
		"stateId": testStateID, "assigneeId": testUserID, "labelIds": []any{testLabelID}, "priority": float64(2), "dueDate": "2026-02-18",
		"parentId": "ENG-1",
	}, input)
	require.Equal(t, issueID, first.Value.IssueID)
	require.Equal(t, "ENG-8", first.Value.Issue.Identifier)
	require.False(t, first.Value.IsReplayed)
	require.Equal(t, issueID, first.Receipt.ProviderObjectID)

	again, err := runMutation("create", client.CreateIssue(), validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, issueID, again.Value.IssueID, "the same Step execution sends the same UUID")
	other, err := runMutation("another-create", client.CreateIssue(), validCreateIssueInput())
	require.NoError(t, err)
	require.NotEqual(t, issueID, other.Value.IssueID, "another Step execution creates another issue")
}

// TestCreateIssueReadsBackAnIssueAnEarlierAttemptCreated covers every answer Linear may give a repeated
// client ID; the duplicate-ID error code is undocumented, so none of them is trusted on its own.
func TestCreateIssueReadsBackAnIssueAnEarlierAttemptCreated(t *testing.T) {
	for name, duplicateAnswer := range map[string]func(t *testing.T, response http.ResponseWriter){
		"input error": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
		},
		"server error": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, 500, linearError("INTERNAL_SERVER_ERROR", "internal error"))
		},
		"usage limit": func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, 200, linearError("USAGE_LIMIT_EXCEEDED", "usage limit exceeded"))
		},
		"unsuccessful": func(t *testing.T, response http.ResponseWriter) {
			writeData(t, response, map[string]any{"issueCreate": map[string]any{"success": false, "issue": nil}})
		},
		"lost response": func(t *testing.T, response http.ResponseWriter) {
			hijacker := response.(http.Hijacker)
			connection, _, err := hijacker.Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		},
	} {
		t.Run(name, func(t *testing.T) {
			var issueID string
			provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
				switch request.operationName {
				case "LinearCreateIssue":
					issueID = fmt.Sprint(request.variables["input"].(map[string]any)["id"])
					duplicateAnswer(t, response)
				case "LinearReadIssueSummary":
					require.Equal(t, map[string]any{"id": map[string]any{"eq": issueID}}, request.variables["filter"])
					writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{issueSummaryJSON(issueID, "ENG-8", "Fire drill")}}})
				}
			})
			result, err := runMutation("replayed", newAPIKeyClient(t, provider.URL).CreateIssue(), validCreateIssueInput())
			require.NoError(t, err)
			require.Equal(t, linear.CreateIssueBranchCreated, result.Branch)
			require.True(t, result.Value.IsReplayed)
			require.Equal(t, issueID, result.Value.IssueID)
			require.Equal(t, []string{"LinearCreateIssue", "LinearReadIssueSummary"}, provider.operationNames())
		})
	}
}

func TestCreateIssueRejectionWithoutAnIssueSelectsProviderRejected(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.operationName == "LinearCreateIssue" {
			writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
			return
		}
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{}}})
	})
	result, err := runMutation("rejected", newAPIKeyClient(t, provider.URL).CreateIssue(), validCreateIssueInput())
	require.NoError(t, err)
	require.Equal(t, linear.CreateIssueBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "Linear rejected the input (HTTP 400) [INPUT_ERROR; invalid input]", result.Failure.Message)
	require.Empty(t, result.Value.IssueID)
	require.Equal(t, testTeamID, result.Value.TeamID)
}

func TestCreateIssueRetriesALostCreateThatLinearDidNotStore(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.operationName == "LinearCreateIssue" {
			writeJSON(t, response, 503, `upstream unavailable`)
			return
		}
		writeData(t, response, map[string]any{"issues": map[string]any{"nodes": []any{}}})
	})
	_, err := runMutation("unavailable", newAPIKeyClient(t, provider.URL).CreateIssue(), validCreateIssueInput())
	requireRetry(t, err, sdkgo.FailureAvailability, 0)
	require.Equal(t, []string{"LinearCreateIssue", "LinearReadIssueSummary"}, provider.operationNames())
}

func TestCreateIssueDoesNotReadBackAfterARateLimitOrACredentialRejection(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{
		"rate limit":     {status: 400, body: linearError("RATELIMITED", "ratelimited")},
		"authentication": {status: 401, body: linearError("AUTHENTICATION_ERROR", "authentication error"), branch: linear.CreateIssueBranchProviderRejected},
		"forbidden":      {status: 403, body: linearError("FORBIDDEN", "forbidden"), branch: linear.CreateIssueBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			result, err := runMutation("no-read-"+name, newAPIKeyClient(t, provider.URL).CreateIssue(), validCreateIssueInput())
			require.Equal(t, 1, provider.requestCount())
			if testCase.branch == "" {
				requireRetry(t, err, sdkgo.FailureRateLimit, 0)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestCreateIssueRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	priority := 5
	for name, change := range map[string]func(*linear.CreateIssueInput){
		"missing team":     func(input *linear.CreateIssueInput) { input.TeamID = "" },
		"team key":         func(input *linear.CreateIssueInput) { input.TeamID = "ENG" },
		"blank title":      func(input *linear.CreateIssueInput) { input.Title = "  " },
		"multiline title":  func(input *linear.CreateIssueInput) { input.Title = "a\nb" },
		"priority":         func(input *linear.CreateIssueInput) { input.Priority = &priority },
		"due date":         func(input *linear.CreateIssueInput) { input.DueDate = "18/02/2026" },
		"assignee email":   func(input *linear.CreateIssueInput) { input.AssigneeID = "alice@example.com" },
		"control in body":  func(input *linear.CreateIssueInput) { input.Description = "a\x00b" },
		"duplicate labels": func(input *linear.CreateIssueInput) { input.LabelIDs = []string{testLabelID, testLabelID} },
	} {
		input := validCreateIssueInput()
		change(&input)
		result, err := runMutation("invalid-"+name, newAPIKeyClient(t, provider.URL).CreateIssue(), input)
		require.NoError(t, err, name)
		require.Equal(t, linear.CreateIssueBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, provider.requestCount())
}
