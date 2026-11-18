// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

func TestCreateTaskReadsBackAfterACanResendMarkerFromAResend(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			hijackAndClose(t, response)
		case request.Method == http.MethodGet:
			writeTaskList(t, response, nil)
		case index == 2:
			writeJSON(t, response, http.StatusTooManyRequests, clickupError("RATE_001"))
		default:
			writeJSON(t, response, http.StatusOK, taskJSON(t, testTask{id: "86b2x9z20", name: createdTaskName}))
		}
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("create-resend-rate-limited")
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		dexContext.nextAttempt()
	}
	require.Equal(t, singledispatch.StateCanResend, lastFenceMarker(t, dexContext).State)
	require.Equal(t, 3, provider.requestCount())

	result, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, clickup.CreateTaskBranchCreated, result.Branch)
	require.Equal(t, http.MethodGet, provider.request(3).method, "the first send may have applied, so the attempt reads back first")
	require.Equal(t, http.MethodPost, provider.request(4).method)
	require.Equal(t, 5, provider.requestCount())
}

func TestCreateTaskReadBackOfAMissingListAfterAPossibleSendIsUncertain(t *testing.T) {
	for name, respond := range map[string]func(http.ResponseWriter){
		"404": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, clickupError("ITEM_013"))
		},
		"401 Workspace not authorized": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnauthorized, clickupError("OAUTH_027"))
		},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					hijackAndClose(t, response)
					return
				}
				respond(response)
			})
			client := newClickUpClient(t, provider.URL)
			dexContext := newTestDexContext("create-missing-list-" + name)
			_, err := sdkgo.RunMutation(dexContext, client.CreateTask(), clickupConnection, validCreateTaskInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, clickup.CreateTaskBranchUncertain, result.Branch, "the first send may have created the task")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestAcceptedCreateWithoutATaskIDIsUncertainAndNeverResent(t *testing.T) {
	for name, body := range map[string]string{
		"invalid JSON":  `{"id":`,
		"no task ID":    `{"name":"Escalation"}`,
		"oversized 2xx": `{"id":"86b2x9z11","name":"` + string(make([]byte, 512)) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			client, err := clickup.New(clickup.Config{MaxResponseBytes: 256}, testCredentialProvider(), clickup.WithAPIBaseURL(provider.URL))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(newTestDexContext("create-accepted-unusable"), client.CreateTask(), clickupConnection, validCreateTaskInput())
			require.NoError(t, err)
			require.Equal(t, clickup.CreateTaskBranchUncertain, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreateTaskCredentialFailureAfterAPossibleSendRetriesWithoutNotSent(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		hijackAndClose(t, response)
	})
	dexContext := newTestDexContext("create-credentials-after-send")
	_, err := sdkgo.RunMutation(dexContext, newClickUpClient(t, provider.URL).CreateTask(), clickupConnection, validCreateTaskInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)

	withoutCredentials, err := clickup.New(clickup.Config{}, sdkgo.StaticCredentialProvider[clickup.Credentials]{}, clickup.WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(dexContext.nextAttempt(), withoutCredentials.CreateTask(), clickupConnection, validCreateTaskInput())
	require.ErrorAs(t, err, &retry, "an earlier send may have applied, so the attempt never selects defect")
	require.Equal(t, singledispatch.StateClaimed, lastFenceMarker(t, dexContext).State, "notSent would hide the earlier send")
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentReadBackOfAMissingTaskAfterAPossibleSendIsUncertain(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			hijackAndClose(t, response)
			return
		}
		writeJSON(t, response, http.StatusNotFound, clickupError("ITEM_013"))
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("comment-missing-after-send")
	input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
	_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(dexContext.nextAttempt(), client.AddComment(), clickupConnection, input)
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
}

func TestAddCommentReadsBackAfterACanResendMarkerFromAResend(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			hijackAndClose(t, response)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, `{"comments":[]}`)
		case index == 2:
			writeJSON(t, response, http.StatusServiceUnavailable, clickupError("SERVER_001"))
		default:
			writeJSON(t, response, http.StatusOK, `{"id":"458","date":"1767225600000"}`)
		}
	})
	client := newClickUpClient(t, provider.URL)
	dexContext := newTestDexContext("comment-resend-unavailable")
	input := clickup.AddCommentInput{TaskID: testTaskID, Text: commentText}
	for attempt := 1; attempt <= 2; attempt++ {
		_, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		dexContext.nextAttempt()
	}
	result, err := sdkgo.RunMutation(dexContext, client.AddComment(), clickupConnection, input)
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchAdded, result.Branch)
	require.Equal(t, http.MethodGet, provider.request(3).method, "the first send may have applied, so the attempt reads back first")
	require.Equal(t, 5, provider.requestCount())
}

func TestAcceptedCommentWithoutACommentIDIsUncertainAndNeverResent(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"date":"1767225600000"}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("comment-accepted-without-id"), newClickUpClient(t, provider.URL).AddComment(), clickupConnection,
		clickup.AddCommentInput{TaskID: testTaskID, Text: commentText})
	require.NoError(t, err)
	require.Equal(t, clickup.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestUpdateTaskRetriesAnUnusableAcceptedResponse(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":7}`)
	})
	name := "Updated login"
	_, err := sdkgo.RunMutation(newTestDexContext("update-unusable"), newClickUpClient(t, provider.URL).UpdateTask(), clickupConnection,
		clickup.UpdateTaskInput{TaskID: testTaskID, Name: &name})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "repeating the absolute update is safe")
}
