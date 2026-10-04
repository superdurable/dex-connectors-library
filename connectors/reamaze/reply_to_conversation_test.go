// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestReplyToConversationSendsAnInternalNoteWithTheDispatchKeyAsOriginID(t *testing.T) {
	var sentOriginID string
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		var payload struct {
			Message struct {
				OriginID string `json:"origin_id"`
			} `json:"message"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		sentOriginID = payload.Message.OriginID
		writeValue(t, response, http.StatusCreated, messageJSON("Dex triage note.", 1, sentOriginID))
	})
	ctx := newReamazeDexContext("note")
	result, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).ReplyToConversation(), reamazeConnection, reamaze.ReplyToConversationInput{
		ConversationID: "double-charge", Text: "Dex triage note.", IsInternalNote: true, ShouldSuppressAutoResolve: true,
	})
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchReplied, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v1/conversations/double-charge/messages", request.path)
	key := dispatchKeyOf(result.Receipt.CallID)
	require.JSONEq(t, `{"message":{"body":"Dex triage note.","visibility":1,"origin_id":"`+key+`","suppress_autoresolve":true}}`, request.body)
	require.Equal(t, key, sentOriginID)
	require.Equal(t, "double-charge", result.Value.ConversationID)
	require.True(t, result.Value.Message.IsInternalNote)
	require.Equal(t, key, result.Value.Message.OriginID)
	require.False(t, result.Value.WasAlreadyApplied)
	require.JSONEq(t, `{"reamazeDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat))
}

func TestReplyToConversationSendsAPublicReplyByDefault(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, messageJSON("We refunded order 88213.", 0, "dex-x"))
	})
	result, err := sdkgo.RunMutation(newReamazeDexContext("reply"), newReamazeClient(t, provider.URL).ReplyToConversation(), reamazeConnection,
		reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "We refunded order 88213.", ShouldSuppressNotifications: true})
	require.NoError(t, err)
	require.JSONEq(t, `{"message":{"body":"We refunded order 88213.","visibility":0,"origin_id":"`+dispatchKeyOf(result.Receipt.CallID)+`",
		"suppress_notifications":true}}`, provider.request(0).body)
	require.False(t, result.Value.Message.IsInternalNote)
}

func TestReplyToConversationReconcilesByOriginIDWithoutResending(t *testing.T) {
	for _, test := range []struct {
		name        string
		isApplied   bool
		wantBranch  sdkgo.BranchID
		wantAlready bool
	}{
		{name: "lost response, note added", isApplied: true, wantBranch: reamaze.ReplyToConversationBranchReplied, wantAlready: true},
		{name: "lost response, nothing added", isApplied: false, wantBranch: reamaze.ReplyToConversationBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			var appliedOriginID string
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					var payload struct {
						Message struct {
							OriginID string `json:"origin_id"`
						} `json:"message"`
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
					if test.isApplied {
						appliedOriginID = payload.Message.OriginID
					}
					dropConnection(t, response)
					return
				}
				messages := []any{messageJSON("Customer follow-up", 0, "customer-1")}
				if appliedOriginID != "" {
					messages = append([]any{messageJSON("Dex triage note.", 1, appliedOriginID)}, messages...)
				}
				writeValue(t, response, http.StatusOK, pageJSON("messages", messages, 1))
			})
			client := newReamazeClient(t, provider.URL)
			input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note.", IsInternalNote: true}
			first := newReamazeDexContext("note-reconcile")
			_, err := sdkgo.RunMutation(first, client.ReplyToConversation(), reamazeConnection, input)
			require.Error(t, err, "a lost response is retried as a reconciliation")
			result, err := sdkgo.RunMutation(first.nextAttempt(), client.ReplyToConversation(), reamazeConnection, input)
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantAlready, result.Value.WasAlreadyApplied)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/api/v1/conversations/double-charge/messages", provider.request(1).path)
			if test.isApplied {
				require.Equal(t, dispatchKeyOf(result.Receipt.CallID), result.Value.Message.OriginID)
			}
		})
	}
}

func TestReplyToConversationSelectsUncertainForUnusableCredentialsAfterAnEarlierDispatch(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		dropConnection(t, response)
	})
	client, credentials := newSwitchingReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note."}
	first := newReamazeDexContext("note-credentials")
	_, err := sdkgo.RunMutation(first, client.ReplyToConversation(), reamazeConnection, input)
	require.Error(t, err, "a lost response keeps the marker and retries")

	credentials.resolveErr = sdkgo.ErrReauthorizationRequired
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.ReplyToConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchUncertain, result.Branch, "a sent reply is never reported as defect")
	require.Equal(t, 1, provider.requestCount())
}

func TestReplyToConversationTerminalOutcomes(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusNotFound, `{"error":"SENTINEL"}`)
		case 1:
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":"SENTINEL"}`)
		default:
			writeValue(t, response, http.StatusCreated, map[string]any{"body": "x", "visibility": 1, "created_at": "2026-01-27T12:00:00Z",
				"conversation": map[string]any{"slug": "another-conversation"}})
		}
	})
	client := newReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Note.", IsInternalNote: true}
	for _, want := range []sdkgo.BranchID{
		reamaze.ReplyToConversationBranchNotFound, reamaze.ReplyToConversationBranchProviderRejected, reamaze.ReplyToConversationBranchUncertain,
	} {
		result, err := sdkgo.RunMutation(newReamazeDexContext("note-terminal"), client.ReplyToConversation(), reamazeConnection, input)
		require.NoError(t, err)
		require.Equal(t, want, result.Branch)
		require.NotContains(t, result.Failure.Message, "SENTINEL")
	}
	for _, invalid := range []reamaze.ReplyToConversationInput{
		{ConversationID: "../x", Text: "Note."},
		{ConversationID: "double-charge", Text: " "},
	} {
		result, err := sdkgo.RunMutation(newReamazeDexContext("note-invalid"), client.ReplyToConversation(), reamazeConnection, invalid)
		require.NoError(t, err)
		require.Equal(t, reamaze.ReplyToConversationBranchDefect, result.Branch)
	}
	require.Equal(t, 3, provider.requestCount())
}
