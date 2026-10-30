// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateConversationWritesOnlyChangedFieldsAndTheCompleteTagList(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			conversation := conversationJSON("double-charge", 1, []string{"Billing", "vip"})
			conversation["assignee"] = map[string]any{"name": "Other", "email": "other@acme.example.com"}
			writeValue(t, response, http.StatusOK, conversation)
			return
		}
		writeValue(t, response, http.StatusOK, conversationJSON("double-charge", 0, []string{"vip", "dex-repeat-contact"}))
	})
	open := reamaze.ConversationStatusOpen
	result, err := sdkgo.RunMutation(newReamazeDexContext("update"), newReamazeClient(t, provider.URL).UpdateConversation(), reamazeConnection,
		reamaze.UpdateConversationInput{
			ConversationID: "double-charge", Status: &open, AssigneeEmail: "agent@acme.example.com",
			AddTags: []string{"dex-repeat-contact", "VIP"}, RemoveTags: []string{"billing"},
		})
	require.NoError(t, err)
	require.Equal(t, reamaze.UpdateConversationBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, reamaze.ConversationStatusOpen, result.Value.Conversation.Status)
	write := provider.request(1)
	require.Equal(t, http.MethodPut, write.method)
	require.Equal(t, "/api/v1/conversations/double-charge", write.path)
	require.JSONEq(t, `{"conversation":{"status":0,"assignee":{"email":"agent@acme.example.com"},"tag_list":["vip","dex-repeat-contact"]}}`, write.body)
}

func TestUpdateConversationWritesNothingWhenAlreadyApplied(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		conversation := conversationJSON("double-charge", 0, []string{"vip", "dex-repeat-contact"})
		conversation["assignee"] = map[string]any{"name": "Agent", "email": "Agent@Acme.example.com"}
		writeValue(t, response, http.StatusOK, conversation)
	})
	open := reamaze.ConversationStatusOpen
	result, err := sdkgo.RunMutation(newReamazeDexContext("update-applied"), newReamazeClient(t, provider.URL).UpdateConversation(), reamazeConnection,
		reamaze.UpdateConversationInput{ConversationID: "double-charge", Status: &open, AssigneeEmail: "agent@acme.example.com",
			AddTags: []string{"DEX-REPEAT-CONTACT"}, RemoveTags: []string{"billing"}})
	require.NoError(t, err)
	require.Equal(t, reamaze.UpdateConversationBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 1, provider.requestCount(), "nothing is written")
}

func TestUpdateConversationAlwaysWritesAnOnHoldReminder(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, conversationJSON("double-charge", 5, nil))
	})
	onHold := reamaze.ConversationStatusOnHold
	result, err := sdkgo.RunMutation(newReamazeDexContext("update-hold"), newReamazeClient(t, provider.URL).UpdateConversation(), reamazeConnection,
		reamaze.UpdateConversationInput{ConversationID: "double-charge", Status: &onHold, HoldUntil: "2026-07-15T09:00:00Z"})
	require.NoError(t, err)
	require.False(t, result.Value.WasAlreadyApplied)
	require.JSONEq(t, `{"conversation":{"status":5,"hold_until":"2026-07-15T09:00:00Z"}}`, provider.request(1).body)
}

func TestUpdateConversationOutcomes(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			writeJSON(t, response, http.StatusNotFound, `{"error":"SENTINEL"}`)
		case request.Method == http.MethodGet:
			writeValue(t, response, http.StatusOK, conversationJSON("double-charge", 1, nil))
		case index == 2:
			dropConnection(t, response)
		default:
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":"SENTINEL not a staff user"}`)
		}
	})
	client := newReamazeClient(t, provider.URL)
	input := reamaze.UpdateConversationInput{ConversationID: "double-charge", AssigneeEmail: "nobody@acme.example.com"}
	result, err := sdkgo.RunMutation(newReamazeDexContext("update-missing"), client.UpdateConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.UpdateConversationBranchNotFound, result.Branch)
	_, err = sdkgo.RunMutation(newReamazeDexContext("update-lost"), client.UpdateConversation(), reamazeConnection, input)
	require.Error(t, err, "a lost write response is retried; the retry reads first")
	result, err = sdkgo.RunMutation(newReamazeDexContext("update-rejected"), client.UpdateConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.UpdateConversationBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}

func TestUpdateConversationRejectsInvalidChangesWithoutARequest(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, conversationJSON("double-charge", 1, nil))
	})
	done := reamaze.ConversationStatusDone
	unknown := reamaze.ConversationStatus(-1)
	for name, input := range map[string]reamaze.UpdateConversationInput{
		"no change":           {ConversationID: "double-charge"},
		"unsafe slug":         {ConversationID: "a/b", Status: &done},
		"unknown status":      {ConversationID: "double-charge", Status: &unknown},
		"hold without status": {ConversationID: "double-charge", HoldUntil: "2026-07-15T09:00:00Z"},
		"hold with done":      {ConversationID: "double-charge", Status: &done, HoldUntil: "2026-07-15T09:00:00Z"},
		"display assignee":    {ConversationID: "double-charge", AssigneeEmail: "Agent <agent@acme.example.com>"},
		"add and remove":      {ConversationID: "double-charge", AddTags: []string{"vip"}, RemoveTags: []string{"VIP"}},
	} {
		result, err := sdkgo.RunMutation(newReamazeDexContext("update-invalid"), newReamazeClient(t, provider.URL).UpdateConversation(), reamazeConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, reamaze.UpdateConversationBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
