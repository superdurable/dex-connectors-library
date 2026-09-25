// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var slackConnection = sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}

func TestListThreadMessagesUsesUserTokenAndReturnsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/conversations.replies", request.URL.Path)
		require.Equal(t, "Bearer user-token", request.Header.Get("Authorization"))
		require.Equal(t, "C123", request.URL.Query().Get("channel"))
		require.Equal(t, "15", request.URL.Query().Get("limit"))
		response.Header().Set("X-Slack-Req-Id", "request-1")
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"root"},{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"reply"}],"response_metadata":{"next_cursor":"next"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-thread"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchRead, result.Branch)
	require.Len(t, result.Value.Messages, 2)
	require.Equal(t, "next", result.Value.NextCursor)
	require.Equal(t, "request-1", result.Receipt.ProviderRequestID)
}

func TestGetThreadReplyReturnsNotFoundForAnotherTimestamp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "2.0", request.URL.Query().Get("oldest"))
		require.Equal(t, "true", request.URL.Query().Get("inclusive"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"3.0","thread_ts":"1.0","user":"U2","text":"other"}]}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchNotFound, result.Branch)
}

func TestGetThreadReplyReturnsExactReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"approve"}]}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply-found"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchFound, result.Branch)
	require.Equal(t, "approve", result.Value.Message.Text)
}

func TestGetThreadReplyProviderRejectionUsesProviderRejectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply-rejected"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestListThreadMessagesInvalidResponseUsesInvalidResponseBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-thread-invalid-response"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func TestPostThreadReplyUsesBotTokenThreadAndStableClientMessageID(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0","message":{"ts":"2.0","thread_ts":"1.0","user":"UBOT","text":"Processing complete"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("reply-complete"), client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "Processing complete"})
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.Equal(t, "1.0", payload["thread_ts"])
	require.Equal(t, string(result.Receipt.IdempotencyKey), payload["client_msg_id"])
	require.NotEmpty(t, payload["client_msg_id"])
}

func TestPostChannelMessageOmitsThreadTimestamp(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"1.0","message":{"ts":"1.0","user":"UBOT","text":"new request"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-root"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "new request"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.NotContains(t, payload, "thread_ts")
	require.Equal(t, "1.0", result.Value.Message.Timestamp)
}

func TestPostMessageProviderRejectionUsesProviderRejectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-rejected"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C404", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
}

func TestPostMessageServerFailureIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = response.Write([]byte(`{"ok":false,"error":"fatal_error"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-unknown"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
}

func TestListThreadMessagesRejectsPageLargerThanSlackLimit(t *testing.T) {
	client := newSlackClient(t, "http://127.0.0.1:1")
	result, err := sdkgo.RunQuery(newSlackDexContext("invalid-page"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 16})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchDefect, result.Branch)
}

func newSlackClient(t *testing.T, endpoint string) *slack.Client {
	t.Helper()
	client, err := slack.New(slack.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[slack.Credentials]{
		slackConnection: {
			BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token"),
		},
	})
	require.NoError(t, err)
	return client
}

type slackDexContext struct {
	context.Context
	step string
}

func newSlackDexContext(step string) *slackDexContext {
	return &slackDexContext{Context: context.Background(), step: step}
}

func (*slackDexContext) FlowID() string                                  { return "slack-flow" }
func (*slackDexContext) RunID() string                                   { return "run" }
func (*slackDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *slackDexContext) StepExecutionID() string                 { return context.step }
func (*slackDexContext) FromStepExecutionID() string                     { return "" }
func (*slackDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*slackDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*slackDexContext) Attempt() int32                                  { return 1 }
func (*slackDexContext) HasTimerFired() bool                             { return false }
func (*slackDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*slackDexContext) WaitForMethodFailed() bool                       { return false }
func (*slackDexContext) RecordHeartbeat(any) error                       { return nil }
func (*slackDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*slackDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*slackDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*slackDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*slackDexContext)(nil)
