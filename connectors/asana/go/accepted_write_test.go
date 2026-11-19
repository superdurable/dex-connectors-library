// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// writeTruncatedBody sends the status and headers, then closes before the promised body arrives.
func writeTruncatedBody(t *testing.T, response http.ResponseWriter, status int) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json")
	response.Header().Set("Content-Length", strconv.Itoa(64))
	response.WriteHeader(status)
	_, err := response.Write([]byte(`{"data":`))
	require.NoError(t, err)
	hijacker, ok := response.(http.Hijacker)
	require.True(t, ok)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

func TestAcceptedCreateWithAnUnreadableBodyIsUncertainAndNeverResent(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeTruncatedBody(t, response, http.StatusCreated)
	})
	result, err := sdkgo.RunMutation(newAsanaDexContext("create-truncated"), newAsanaClient(t, provider.URL).CreateTask(), asanaConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAcceptedCommentWithAnUnreadableBodyIsUncertainAndNeverResent(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeTruncatedBody(t, response, http.StatusCreated)
	})
	result, err := sdkgo.RunMutation(newAsanaDexContext("comment-truncated"), newAsanaClient(t, provider.URL).AddComment(), asanaConnection,
		asana.AddCommentInput{TaskID: testTaskID, Text: "Approved."})
	require.NoError(t, err)
	require.Equal(t, asana.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTaskRefusalWithAnUnreadableBodyIsAnUnknownOutcome(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeTruncatedBody(t, response, http.StatusBadRequest)
	})
	_, err := sdkgo.RunMutation(newAsanaDexContext("create-refusal-truncated"), newAsanaClient(t, provider.URL).CreateTask(), asanaConnection, validCreateTaskInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
}

func TestAcceptedCreateWithAUsableIDIsReadBackInsteadOfResent(t *testing.T) {
	provider := newRecordingAsana(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, `{"data":{"gid":"`+testTaskID+`","created_at":7}}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"data":{"gid":"`+testTaskID+`","permalink_url":"https://app.asana.com/0/1/`+testTaskID+`"}}`)
	})
	client := newAsanaClient(t, provider.URL)
	first := newReplayAsanaDexContext("create-unusable-fields", 1, nil)
	_, err := sdkgo.RunMutation(first, client.CreateTask(), asanaConnection, validCreateTaskInput())
	requireRetry(t, err, sdkgo.FailureProtocol)
	second := newReplayAsanaDexContext("create-unusable-fields", 2, first.lastHeartbeat())
	result, err := sdkgo.RunMutation(second, client.CreateTask(), asanaConnection, validCreateTaskInput())
	require.NoError(t, err)
	require.Equal(t, asana.CreateTaskBranchCreated, result.Branch)
	require.Equal(t, testTaskID, result.Value.TaskID)
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.Equal(t, 2, provider.requestCount())
}
