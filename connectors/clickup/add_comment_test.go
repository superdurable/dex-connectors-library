// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const commentText = "Escalated to Ada in https://app.clickup.com/t/86b2x9z11.\nPlease reply there."

func TestAddCommentSendsPlainTextAfterRecordingACheckpoint(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":90140001234,"hist_id":"26508","date":1767225600000}`)
	})
	dexContext := newTestDexContext("comment")
	result, err := sdkgo.RunMutation(dexContext, newClickUpClient(t, provider.URL).AddComment(), clickupConnection,
		clickup.AddCommentInput{TaskID: testTaskID, Text: commentText})
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchAdded, result.Branch)
	require.Equal(t, "90140001234", result.Value.CommentID, "a numeric comment ID is kept as text")
	require.Equal(t, time.UnixMilli(1767225600000).UTC(), *result.Value.CreatedAt)
	require.Equal(t, "90140001234", result.Receipt.Metadata["commentId"])
	require.True(t, dexContext.hasHeartbeat())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/task/"+testTaskID+"/comment", request.path)
	encodedText, err := json.Marshal(commentText)
	require.NoError(t, err)
	require.JSONEq(t, `{"comment_text":`+string(encodedText)+`,"notify_all":false}`, request.body)
}

func TestUnconfirmedCommentIsReconciledFromTheTaskAndNeverResent(t *testing.T) {
	dispatchedAt := time.UnixMilli(1767225600000)
	for _, test := range []struct {
		name           string
		comments       string
		expectedBranch sdkgo.BranchID
	}{
		{name: "matching comment", comments: `[{"id":"458","comment_text":"Escalated to Ada in https://app.clickup.com/t/86b2x9z11. Please reply there.\n","date":"1767225601000"},
			{"id":"457","comment_text":"Older","date":"1767225000000"}]`, expectedBranch: clickup.AddCommentBranchAdded},
		{name: "no matching comment", comments: `[{"id":"457","comment_text":"Older","date":"1767225000000"}]`, expectedBranch: clickup.AddCommentBranchUncertain},
		{name: "matching comment before the dispatch", comments: `[{"id":"456","comment_text":"` + "Escalated to Ada in https://app.clickup.com/t/86b2x9z11. Please reply there." +
			`","date":"1767225000000"}]`, expectedBranch: clickup.AddCommentBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusBadGateway, clickupError("GATEWAY_1"))
					return
				}
				writeJSON(t, response, http.StatusOK, `{"comments":`+test.comments+`}`)
			})
			client := newClickUpClient(t, provider.URL, clickup.WithClock(func() time.Time { return dispatchedAt.Add(4 * time.Minute) }))
			dexContext := newTestDexContext("comment-" + test.name)
			input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
			_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a 5xx after dispatch retries with the checkpoint held")

			result, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			if test.expectedBranch == clickup.AddCommentBranchAdded {
				require.Equal(t, "458", result.Value.CommentID)
				require.True(t, result.Value.WasAlreadyApplied)
			} else {
				require.Empty(t, result.Value.CommentID)
				require.Contains(t, result.Failure.Message, "so it is not sent again")
			}
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/task/"+testTaskID+"/comment", provider.request(1).path)
		})
	}
}

func TestAddCommentToAMissingTaskIsNotFound(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, clickupError("ITEM_013"))
	})
	result, err := sdkgo.RunMutation(newTestDexContext("comment-missing"), newClickUpClient(t, provider.URL).AddComment(), clickupConnection,
		clickup.AddCommentInput{TaskID: testTaskID, Text: commentText})
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchNotFound, result.Branch)
	require.NotContains(t, result.Failure.Message, providerSentinel)
}
