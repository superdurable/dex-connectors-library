// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	jiraservicemanagement "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira-service-management"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddCommentSendsAPublicReplyOrAnInternalNote(t *testing.T) {
	for _, isPublic := range []bool{true, false} {
		provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			visibility := "false"
			if isPublic {
				visibility = "true"
			}
			writeJSON(t, response, http.StatusCreated, `{"id":"1000","body":"SENTINEL echo","public":`+visibility+`,`+
				`"author":{"accountId":"5b10ac8d82e05b22cc7d4ef5"},"created":{"epochMillis":1790000000000}}`)
		})
		ctx := newTestDexContext("comment")
		result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).AddComment(), jsmConnection, jiraservicemanagement.AddCommentInput{
			IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: isPublic,
		})
		require.NoError(t, err)
		require.Equal(t, jiraservicemanagement.AddCommentBranchAdded, result.Branch)
		require.Equal(t, "1000", result.Value.CommentID)
		require.Equal(t, isPublic, result.Value.IsPublic)
		request := provider.request(0)
		require.Equal(t, http.MethodPost, request.method)
		require.Equal(t, testServiceDeskPath+"/request/ITH-42/comment", request.path)
		if isPublic {
			require.JSONEq(t, `{"body":"We are on it.","public":true}`, request.body)
		} else {
			require.JSONEq(t, `{"body":"We are on it.","public":false}`, request.body, "an internal note sends public false explicitly")
		}
		require.NotEmpty(t, ctx.recordedHeartbeat)
		requireNoSentinel(t, result)
	}
}

func TestAddCommentSelectsNotFoundAndUncertain(t *testing.T) {
	missing := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errorMessage":"SENTINEL Request does not exist"}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("comment-missing"), newTestClient(t, missing.URL).AddComment(), jsmConnection,
		jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-99", Body: "Hello"})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchNotFound, result.Branch)
	requireNoSentinel(t, result)

	lost := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	ctx := newTestDexContext("comment-lost")
	result, err = sdkgo.RunMutation(ctx, newTestClient(t, lost.URL).AddComment(), jsmConnection,
		jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "Hello", IsPublic: true})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, result.Branch)
	require.True(t, result.Value.IsPublic)
	replay, err := sdkgo.RunMutation(ctx.nextAttempt(), newTestClient(t, lost.URL).AddComment(), jsmConnection,
		jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "Hello", IsPublic: true})
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, replay.Branch)
	require.Equal(t, 1, lost.requestCount(), "a customer never receives the reply twice from one Step execution")
}

func TestAddCommentRejectsABlankBodyWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []jiraservicemanagement.AddCommentInput{
		{IssueIDOrKey: "ITH-42", Body: "  "},
		{IssueIDOrKey: "ITH 42", Body: "Hello"},
		{IssueIDOrKey: "ITH-42", Body: "bell \a"},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("comment-invalid"), newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
		require.NoError(t, err)
		require.Equal(t, jiraservicemanagement.AddCommentBranchDefect, result.Branch)
	}
}
