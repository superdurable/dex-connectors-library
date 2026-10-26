// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement_test

import (
	"net/http"
	"strings"
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

func TestAddCommentRetries503AndFailsClosedAfterAnAcceptedReply(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errorMessage":"SENTINEL unavailable"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"id":"1001","public":true}`)
	})
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	ctx := newTestDexContext("comment-503")
	_, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, string(ctx.recordedHeartbeat), `"canResend":true`)
	next := ctx.nextAttempt()
	result, err := sdkgo.RunMutation(next, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchAdded, result.Branch)
	third := next.nextAttempt()
	third.previousHeartbeat = ctx.recordedHeartbeat
	result, err = sdkgo.RunMutation(third, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount(), "an accepted reply is never resent")
}

func TestAddCommentKeepsAConflictUncertainWithoutResending(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusConflict, `{"errorMessage":"SENTINEL conflict"}`)
	})
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	ctx := newTestDexContext("comment-conflict")
	result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	replay, err := sdkgo.RunMutation(ctx.nextAttempt(), newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, replay.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentDoesNotResendAfterAnInterruptedAcceptedResponse(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeInterruptedAcceptedJSON(t, response)
	})
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	ctx := newTestDexContext("comment-interrupted-accepted")
	result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, result.Branch)
	replay, err := sdkgo.RunMutation(ctx.nextAttempt(), newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, replay.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentDoesNotResendAfterAnOversizedAcceptedResponse(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"1001","padding":"`+strings.Repeat("x", 2048)+`"}`)
	})
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{
		CloudID: testCloudID, Endpoint: provider.URL, MaxResponseBytes: 1024,
	}, staticTestCredentials())
	require.NoError(t, err)
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	ctx := newTestDexContext("comment-oversized-accepted")
	result, err := sdkgo.RunMutation(ctx, client.AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, result.Branch)
	replay, err := sdkgo.RunMutation(ctx.nextAttempt(), client.AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, replay.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentChecksPriorDispatchBeforeResolvingCredentials(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"1001","public":true}`)
	})
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	ctx := newTestDexContext("comment-credential-after-acceptance")
	result, err := sdkgo.RunMutation(ctx, newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchAdded, result.Branch)
	client, err := jiraservicemanagement.New(jiraservicemanagement.Config{CloudID: testCloudID, Endpoint: provider.URL},
		sdkgo.StaticCredentialProvider[jiraservicemanagement.Credentials]{})
	require.NoError(t, err)
	replay, err := sdkgo.RunMutation(ctx.nextAttempt(), client.AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchUncertain, replay.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddCommentReadsBackAConflictWithACommentID(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusConflict, `{"id":"1001"}`)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, testServiceDeskPath+"/request/ITH-42/comment/1001", request.URL.Path)
		writeJSON(t, response, http.StatusOK, `{"id":"1001","body":"We are on it.","public":true}`)
	})
	input := jiraservicemanagement.AddCommentInput{IssueIDOrKey: "ITH-42", Body: "We are on it.", IsPublic: true}
	result, err := sdkgo.RunMutation(newTestDexContext("comment-conflict-readback"), newTestClient(t, provider.URL).AddComment(), jsmConnection, input)
	require.NoError(t, err)
	require.Equal(t, jiraservicemanagement.AddCommentBranchAdded, result.Branch)
	require.Equal(t, "1001", result.Value.CommentID)
	require.Equal(t, 2, provider.requestCount())
}
