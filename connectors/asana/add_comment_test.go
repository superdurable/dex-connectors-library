// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddCommentPostsPlainTextAndReturnsTheStory(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"data":{"gid":"1205000000000001","resource_type":"story","resource_subtype":"comment_added",`+
			`"created_at":"2026-09-30T16:20:00.000Z","created_by":{"gid":"`+testUserID+`","name":"Ada Lovelace"},"text":"Approved by Grace."}}`)
	})
	client := newAsanaClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newAsanaDexContext("comment"), client.AddComment(), asanaConnection,
		asana.AddCommentInput{TaskID: testTaskID, Text: "Approved by Grace.\n<b>not markup</b>"})
	require.NoError(t, err)
	require.Equal(t, asana.AddCommentBranchAdded, result.Branch)
	createdAt := time.Date(2026, time.September, 30, 16, 20, 0, 0, time.UTC)
	require.Equal(t, asana.AddCommentOutput{
		TaskID: testTaskID, CommentID: "1205000000000001", Author: &asana.UserReference{ID: testUserID, Name: "Ada Lovelace"}, CreatedAt: &createdAt,
	}, result.Value)
	require.Equal(t, "1205000000000001", result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/tasks/"+testTaskID+"/stories", request.path)
	require.JSONEq(t, `{"data":{"text":"Approved by Grace.\n<b>not markup</b>"}}`, request.body, "plain text is sent as text, never html_text")
}

func TestAddCommentNeverResendsARequestAsanaMayHaveReceived(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "missing task", status: http.StatusNotFound, branch: asana.AddCommentBranchNotFound},
		{name: "permission", status: http.StatusForbidden, branch: asana.AddCommentBranchProviderRejected},
		{name: "rate limit", status: http.StatusTooManyRequests, isRetry: true},
		{name: "server error", status: http.StatusInternalServerError, branch: asana.AddCommentBranchUncertain},
		{name: "unusable story", status: http.StatusCreated, branch: asana.AddCommentBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, `{"data":{"gid":"SENTINEL"},"errors":[{"message":"SENTINEL"}]}`)
			})
			client := newAsanaClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newAsanaDexContext("comment-"+test.name), client.AddComment(), asanaConnection,
				asana.AddCommentInput{TaskID: testTaskID, Text: "Approved."})
			require.Equal(t, 1, provider.requestCount())
			if test.isRetry {
				requireRetry(t, err, sdkgo.FailureRateLimit)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Empty(t, result.Value.CommentID)
			require.Equal(t, testTaskID, result.Value.TaskID)
			requireNoSentinel(t, result)
		})
	}
}

func TestAddCommentTimeoutAfterDispatchIsUncertain(t *testing.T) {
	provider := newRecordingAsana(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newAsanaClient(t, provider.URL, asana.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	result, err := sdkgo.RunMutation(newAsanaDexContext("comment-timeout"), client.AddComment(), asanaConnection,
		asana.AddCommentInput{TaskID: testTaskID, Text: "Approved."})
	require.NoError(t, err)
	require.Equal(t, asana.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingAsana(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newAsanaClient(t, provider.URL)
	for _, test := range []struct {
		input   asana.AddCommentInput
		message string
	}{
		{asana.AddCommentInput{TaskID: testTaskID, Text: "  "}, "text is required"},
		{asana.AddCommentInput{TaskID: testTaskID, Text: "bell\a"}, "text cannot contain control characters"},
		{asana.AddCommentInput{TaskID: "task", Text: "Approved."}, "taskId must be an Asana gid, a decimal string such as 1204567890123456"},
	} {
		result, err := sdkgo.RunMutation(newAsanaDexContext("comment-invalid"), client.AddComment(), asanaConnection, test.input)
		require.NoError(t, err)
		require.Equal(t, asana.AddCommentBranchDefect, result.Branch)
		require.Equal(t, test.message, result.Failure.Message)
	}
}
