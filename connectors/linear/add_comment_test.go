// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func commentJSON(commentID string) map[string]any {
	return map[string]any{"id": commentID, "url": "https://linear.app/acme/issue/ENG-7#comment-" + commentID[:8], "createdAt": "2026-01-28T11:00:00.000Z"}
}

func TestAddCommentSendsAClientSuppliedUUID(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.operationName == "LinearCreateIssue" {
			writeData(t, response, createdIssueAnswer(request))
			return
		}
		commentID := fmt.Sprint(request.variables["input"].(map[string]any)["id"])
		writeData(t, response, map[string]any{"commentCreate": map[string]any{"success": true, "comment": commentJSON(commentID)}})
	})
	client := newAPIKeyClient(t, provider.URL)
	result, err := runMutation("comment", client.AddComment(), linear.AddCommentInput{IssueID: "eng-7", Body: "Triaged by **Dex**."})
	require.NoError(t, err)
	require.Equal(t, linear.AddCommentBranchAdded, result.Branch)
	input := provider.request(0).variables["input"].(map[string]any)
	commentID := fmt.Sprint(input["id"])
	require.Regexp(t, uuidV4Pattern, commentID)
	require.Equal(t, map[string]any{"id": commentID, "issueId": "ENG-7", "body": "Triaged by **Dex**."}, input)
	require.Equal(t, commentID, result.Value.CommentID)
	require.Equal(t, "ENG-7", result.Value.IssueID)
	require.NotNil(t, result.Value.CreatedAt)
	require.False(t, result.Value.IsReplayed)

	sameStep, err := runMutation("comment", client.AddComment(), linear.AddCommentInput{IssueID: "eng-7", Body: "Triaged by **Dex**."})
	require.NoError(t, err)
	require.Equal(t, commentID, sameStep.Value.CommentID, "every attempt of one Step sends the same UUID")
	issue, err := runMutation("comment", client.CreateIssue(), validCreateIssueInput())
	require.NoError(t, err)
	require.NotEqual(t, commentID, issue.Value.IssueID, "an issue and a comment of one Step never share a UUID")
}

func TestAddCommentReadsBackACommentAnEarlierAttemptAdded(t *testing.T) {
	var commentID string
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		switch request.operationName {
		case "LinearAddComment":
			commentID = fmt.Sprint(request.variables["input"].(map[string]any)["id"])
			writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
		case "LinearReadComment":
			require.Equal(t, map[string]any{"id": map[string]any{"eq": commentID}}, request.variables["filter"])
			writeData(t, response, map[string]any{"comments": map[string]any{"nodes": []any{commentJSON(commentID)}}})
		}
	})
	result, err := runMutation("replayed", newAPIKeyClient(t, provider.URL).AddComment(), linear.AddCommentInput{IssueID: testIssueID, Body: "Done."})
	require.NoError(t, err)
	require.Equal(t, linear.AddCommentBranchAdded, result.Branch)
	require.True(t, result.Value.IsReplayed)
	require.Equal(t, commentID, result.Value.CommentID)
}

func TestAddCommentRejectionSelectsNotFoundOrProviderRejectedFromTheIssue(t *testing.T) {
	for name, testCase := range map[string]struct {
		issues []any
		branch sdkgo.BranchID
	}{
		"missing issue":  {issues: []any{}, branch: linear.AddCommentBranchNotFound},
		"existing issue": {issues: []any{issueSummaryJSON(testIssueID, "ENG-7", "x")}, branch: linear.AddCommentBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
				switch request.operationName {
				case "LinearAddComment":
					writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
				case "LinearReadComment":
					writeData(t, response, map[string]any{"comments": map[string]any{"nodes": []any{}}})
				default:
					writeData(t, response, map[string]any{"issues": map[string]any{"nodes": testCase.issues}})
				}
			})
			result, err := runMutation("rejected-"+name, newAPIKeyClient(t, provider.URL).AddComment(), linear.AddCommentInput{IssueID: testIssueID, Body: "Done."})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Empty(t, result.Value.CommentID)
			require.Equal(t, []string{"LinearAddComment", "LinearReadComment", "LinearReadIssueSummary"}, provider.operationNames())
		})
	}
}

func TestAddCommentRetriesALostAnswerThatLinearDidNotStore(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.operationName == "LinearAddComment" {
			writeData(t, response, map[string]any{"commentCreate": map[string]any{"success": true, "comment": map[string]any{"id": "another"}}})
			return
		}
		writeData(t, response, map[string]any{"comments": map[string]any{"nodes": []any{}}})
	})
	_, err := runMutation("unusable", newAPIKeyClient(t, provider.URL).AddComment(), linear.AddCommentInput{IssueID: testIssueID, Body: "Done."})
	requireRetry(t, err, sdkgo.FailureProtocol, 0)
	require.Equal(t, []string{"LinearAddComment", "LinearReadComment"}, provider.operationNames())
}

func TestAddCommentRetriesARejectionWhoseReadBackFails(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.operationName == "LinearAddComment" {
			writeJSON(t, response, 400, linearError("INPUT_ERROR", "invalid input"))
			return
		}
		writeJSON(t, response, 403, linearError("FORBIDDEN", "forbidden"))
	})
	_, err := runMutation("unread", newAPIKeyClient(t, provider.URL).AddComment(), linear.AddCommentInput{IssueID: testIssueID, Body: "Done."})
	requireRetry(t, err, sdkgo.FailureAuthorization, 0)
	require.Equal(t, []string{"LinearAddComment", "LinearReadComment"}, provider.operationNames())
}

func TestAddCommentRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	for name, input := range map[string]linear.AddCommentInput{
		"blank body": {IssueID: testIssueID, Body: " \n "},
		"no issue":   {Body: "Done."},
		"bad issue":  {IssueID: "issue 7", Body: "Done."},
	} {
		result, err := runMutation("invalid-"+name, newAPIKeyClient(t, provider.URL).AddComment(), input)
		require.NoError(t, err, name)
		require.Equal(t, linear.AddCommentBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
