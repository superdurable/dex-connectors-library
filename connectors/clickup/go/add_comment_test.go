// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup/go"
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

func TestUnknownCommentReadsTheTaskBackBeforeResending(t *testing.T) {
	firstAttemptAt := time.UnixMilli(1767225600000)
	matchingText := "Escalated to Ada in https://app.clickup.com/t/86b2x9z11. Please reply there."
	for _, test := range []struct {
		name              string
		comments          string
		expectedBranch    sdkgo.BranchID
		expectedCommentID string
		isResent          bool
	}{
		{name: "matching comment", comments: `[{"id":"458","comment_text":"` + matchingText + `\n","date":"1767225601000"},
			{"id":"457","comment_text":"Older","date":"1767225000000"}]`, expectedBranch: clickup.AddCommentBranchAdded, expectedCommentID: "458"},
		{name: "no matching comment", comments: `[{"id":"457","comment_text":"Older","date":"1767225000000"}]`,
			expectedBranch: clickup.AddCommentBranchAdded, expectedCommentID: "90140001234", isResent: true},
		{name: "matching comment before the first attempt", comments: `[{"id":"456","comment_text":"` + matchingText + `","date":"1767225000000"}]`,
			expectedBranch: clickup.AddCommentBranchAdded, expectedCommentID: "90140001234", isResent: true},
		{name: "two matching comments", comments: `[{"id":"458","comment_text":"` + matchingText + `","date":"1767225601000"},
			{"id":"459","comment_text":"` + matchingText + `","date":"1767225602000"}]`, expectedBranch: clickup.AddCommentBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch {
				case index == 0:
					writeJSON(t, response, http.StatusBadGateway, clickupError("GATEWAY_1"))
				case request.Method == http.MethodPost:
					writeJSON(t, response, http.StatusOK, `{"id":90140001234,"hist_id":"26508","date":1767225700000}`)
				default:
					writeJSON(t, response, http.StatusOK, `{"comments":`+test.comments+`}`)
				}
			})
			client := newClickUpClient(t, provider.URL)
			dexContext := newTestDexContext("comment-" + test.name)
			dexContext.firstAttemptAt = firstAttemptAt
			input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
			_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "a 5xx after dispatch retries with the claim held")

			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.AddComment(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedCommentID, result.Value.CommentID)
			require.Equal(t, test.expectedBranch == clickup.AddCommentBranchAdded && !test.isResent, result.Value.WasAlreadyApplied)
			expectedRequests := 2
			if test.isResent {
				expectedRequests = 3
				require.Equal(t, http.MethodPost, provider.request(2).method, "read-back found nothing, so the comment is sent again within the cap")
			}
			require.Equal(t, expectedRequests, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/task/"+testTaskID+"/comment", provider.request(1).path)
		})
	}
}

func TestUnknownCommentSelectsUncertainOnlyAtTheSendCap(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			hijackAndClose(t, response)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("comment-at-cap")
	input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
	for attempt := 1; attempt <= 3; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "an unknown outcome below the cap retries")
		dexContext.nextAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchUncertain, result.Branch)
	posts := 0
	for index := range provider.requestCount() {
		if provider.request(index).method == http.MethodPost {
			posts++
		}
	}
	require.Equal(t, 3, posts, "possible sends never exceed the default cap of three")
}

func TestAcceptedCommentIsReadBackByItsIDAndNeverResent(t *testing.T) {
	for _, test := range []struct {
		name           string
		providerID     string
		expectedBranch sdkgo.BranchID
	}{
		{name: "saved comment found", providerID: "457", expectedBranch: clickup.AddCommentBranchAdded},
		{name: "saved comment missing", providerID: "999", expectedBranch: clickup.AddCommentBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					hijackAndClose(t, response)
					return
				}
				writeJSON(t, response, http.StatusOK, `{"comments":[{"id":"457","comment_text":"Older","date":"1767225000000"}]}`)
			})
			client := newClickUpClient(t, provider.URL)
			dexContext := newTestDexContext("comment-accepted-" + test.name)
			input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
			_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			marker := lastFenceMarker(t, dexContext)
			marker.ProviderID = test.providerID
			dexContext.heartbeat, err = json.Marshal(marker)
			require.NoError(t, err)

			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.AddComment(), clickupConnection, input)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch, "only the saved comment ID matches, whatever its text")
			require.Equal(t, 2, provider.requestCount(), "an identified comment is never sent again")
			if test.expectedBranch == clickup.AddCommentBranchAdded {
				require.Equal(t, "457", result.Value.CommentID)
				require.True(t, result.Value.WasAlreadyApplied)
			}
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
