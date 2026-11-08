// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package telegram

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSendMessageConfirmsOnceThenFailsClosed(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/bot"+testBotToken+"/sendMessage", r.URL.Path)
		require.Equal(t, "@meridian_team", r.PostFormValue("chat_id"))
		require.Equal(t, "Hello from Dex", r.PostFormValue("text"))
		_, err := fmt.Fprint(w, `{"ok":true,"result":{"message_id":52,"date":1794110000,"chat":{"id":-1001234567890}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("send-flow", "send-step")
	input := SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
	require.EqualValues(t, 52, sent.Value.MessageID)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestSendMessageRetries429WithCheckpoint(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, err := fmt.Fprint(w, `{"ok":true,"result":{"message_id":53,"date":1794110001,"chat":{"id":-1001234567890}}}`)
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("retry-flow", "send-step")
	input := SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.Error(t, err)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchSent, sent.Branch)
	require.Equal(t, 2, requests)
}

func TestSendMessageDoesNotRepeatAmbiguousServerError(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("uncertain-flow", "send-step")
	input := SendMessageInput{ChatID: "@meridian_team", Text: "Hello from Dex"}
	uncertain, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	repeated, err := sdkgo.RunMutation(ctx, client.SendMessage(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendMessageBranchUncertain, repeated.Branch)
	require.Equal(t, 1, requests)
}
