// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func approvalButtons() []slack.MessageButton {
	return []slack.MessageButton{
		{ActionID: "approve", Text: "Approve", Value: "order-1042", Style: slack.ButtonStylePrimary},
		{ActionID: "reject", Text: "Reject", Value: "order-1042", Style: slack.ButtonStyleDanger},
		{ActionID: "details", Text: "Ask for details"},
	}
}

func TestPostButtonMessageSendsSectionAndActionsBlocks(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/chat.postMessage", request.URL.Path)
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0","message":{"ts":"2.0","thread_ts":"1.0","user":"UBOT","text":"Approve order 1042?"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-buttons"), client.PostButtonMessage(), slackConnection, slack.PostButtonMessageInput{
		ChannelID: "C123", ThreadTimestamp: "1.0", Text: "Approve *order 1042*?", Buttons: approvalButtons(),
	})
	require.NoError(t, err)
	require.Equal(t, slack.PostButtonMessageBranchSent, result.Branch)
	require.Equal(t, "2.0", result.Value.Message.Timestamp)
	require.Equal(t, "1.0", result.Value.Message.ThreadTimestamp)
	require.Equal(t, string(result.Receipt.IdempotencyKey), payload["client_msg_id"])
	require.Equal(t, "1.0", payload["thread_ts"])
	require.Equal(t, "Approve *order 1042*?", payload["text"], "the text is also the notification fallback")
	require.Equal(t, []any{
		map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": "Approve *order 1042*?"}},
		map[string]any{"type": "actions", "elements": []any{
			map[string]any{"type": "button", "action_id": "approve", "value": "order-1042", "style": "primary",
				"text": map[string]any{"type": "plain_text", "text": "Approve", "emoji": true}},
			map[string]any{"type": "button", "action_id": "reject", "value": "order-1042", "style": "danger",
				"text": map[string]any{"type": "plain_text", "text": "Reject", "emoji": true}},
			map[string]any{"type": "button", "action_id": "details",
				"text": map[string]any{"type": "plain_text", "text": "Ask for details", "emoji": true}},
		}},
	}, payload["blocks"])
}

func TestPostButtonMessageWithoutThreadPostsTopLevelMessage(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"3.0","message":{"ts":"3.0","user":"UBOT","text":"Approve?"}}`))
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(newSlackDexContext("post-buttons-root"), newSlackClient(t, server.URL).PostButtonMessage(), slackConnection,
		slack.PostButtonMessageInput{ChannelID: "C123", Text: "Approve?", Buttons: approvalButtons()[:1]})
	require.NoError(t, err)
	require.Equal(t, slack.PostButtonMessageBranchSent, result.Branch)
	require.NotContains(t, payload, "thread_ts")
}

func TestPostButtonMessageRejectsInvalidButtonsBeforeSending(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	valid := func(change func(*slack.PostButtonMessageInput)) slack.PostButtonMessageInput {
		input := slack.PostButtonMessageInput{ChannelID: "C123", Text: "Approve?", Buttons: approvalButtons()}
		change(&input)
		return input
	}
	tooManyButtons := make([]slack.MessageButton, 26)
	for index := range tooManyButtons {
		tooManyButtons[index] = slack.MessageButton{ActionID: "action-" + strings.Repeat("x", index+1), Text: "Choice"}
	}
	cases := map[string]slack.PostButtonMessageInput{
		"missing channel":       valid(func(input *slack.PostButtonMessageInput) { input.ChannelID = " " }),
		"missing text":          valid(func(input *slack.PostButtonMessageInput) { input.Text = "" }),
		"section text too long": valid(func(input *slack.PostButtonMessageInput) { input.Text = strings.Repeat("a", 3001) }),
		"no buttons":            valid(func(input *slack.PostButtonMessageInput) { input.Buttons = nil }),
		"too many buttons":      valid(func(input *slack.PostButtonMessageInput) { input.Buttons = tooManyButtons }),
		"missing action ID":     valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].ActionID = "" }),
		"long action ID":        valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].ActionID = strings.Repeat("a", 256) }),
		"duplicate action ID":   valid(func(input *slack.PostButtonMessageInput) { input.Buttons[1].ActionID = "approve" }),
		"missing label":         valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].Text = " " }),
		"long label":            valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].Text = strings.Repeat("a", 76) }),
		"long value":            valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].Value = strings.Repeat("a", 2001) }),
		"unknown style":         valid(func(input *slack.PostButtonMessageInput) { input.Buttons[0].Style = "blue" }),
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotNil(t, slack.ValidatePostButtonMessageInput(input))
			result, err := sdkgo.RunMutation(newSlackDexContext("post-buttons-invalid"), client.PostButtonMessage(), slackConnection, input)
			require.NoError(t, err)
			require.Equal(t, slack.PostButtonMessageBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Zero(t, requests, "invalid button messages are never sent")
	require.Nil(t, slack.ValidatePostButtonMessageInput(valid(func(input *slack.PostButtonMessageInput) {
		input.Text = strings.Repeat("a", 3000)
		input.Buttons[0].Value = strings.Repeat("a", 2000)
	})))
	require.Equal(t, "Each button action ID must be unique.",
		slack.ValidatePostButtonMessageInput(cases["duplicate action ID"]).Message, "the duplicateButtonActionID mock uses the same message")
}

func TestPostButtonMessageAppliesConnectionTextLimit(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer server.Close()
	client, err := slack.New(slack.Config{Endpoint: server.URL, MaxMessageCharacters: 5}, sdkgo.StaticCredentialProvider[slack.Credentials]{
		slackConnection: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")},
	})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-buttons-limit"), client.PostButtonMessage(), slackConnection,
		slack.PostButtonMessageInput{ChannelID: "C123", Text: "Approve?", Buttons: approvalButtons()})
	require.NoError(t, err)
	require.Equal(t, slack.PostButtonMessageBranchDefect, result.Branch)
	require.Zero(t, requests)
}
