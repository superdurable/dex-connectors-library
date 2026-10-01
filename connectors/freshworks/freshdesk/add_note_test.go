// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddNoteAddsAPrivateNoteByDefault(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":5,"body":"<div>x</div>","body_text":"Refund issued on Jan 13 <REF-771>.","incoming":false,"private":true,
			"user_id":1,"support_email":null,"ticket_id":3,"notified_to":[],"created_at":"2026-01-27T10:00:00Z","updated_at":"2026-01-27T10:00:00Z"}`)
	})
	ctx := newFreshdeskDexContext("note")
	result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, freshdesk.AddNoteInput{
		TicketID: 3, Body: "Refund issued on Jan 13 <REF-771>.",
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchAdded, result.Branch)
	request := provider.request(0)
	require.Equal(t, "/api/v2/tickets/3/notes", request.path)
	require.JSONEq(t, `{"body":"Refund issued on Jan 13 &lt;REF-771&gt;.","private":true}`, request.body)
	conversation := result.Value.Conversation
	require.Equal(t, int64(5), conversation.ID)
	require.Equal(t, int64(3), conversation.TicketID)
	require.True(t, conversation.IsPrivate)
	require.Equal(t, freshdesk.ConversationSourceNote, conversation.Source)
	require.Equal(t, "Refund issued on Jan 13 <REF-771>.", conversation.Body)
	require.Equal(t, "5", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, ctx.recordedHeartbeat)
}

func TestAddNoteSendsAPublicReplyThroughTheReplyEndpoint(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":4,"body_text":"We are working on this issue.","user_id":1,"from_email":"support@acme.example.com",
			"cc_emails":[],"bcc_emails":[],"ticket_id":141,"replied_to":["SENTINEL@example.com"],"created_at":"2026-01-27T10:00:00Z"}`)
	})
	result, err := sdkgo.RunMutation(newFreshdeskDexContext("reply"), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, freshdesk.AddNoteInput{
		TicketID: 141, Body: "We are working on this issue.", IsPublicReply: true,
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchAdded, result.Branch)
	require.Equal(t, "/api/v2/tickets/141/reply", provider.request(0).path)
	require.JSONEq(t, `{"body":"We are working on this issue."}`, provider.request(0).body)
	require.False(t, result.Value.Conversation.IsPrivate)
	require.Equal(t, freshdesk.ConversationSourceReply, result.Value.Conversation.Source)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "recipient addresses are not carried")
}

func TestAddNoteBranches(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing ticket", status: http.StatusNotFound, branch: freshdesk.AddNoteBranchNotFound},
		{name: "rejected", status: http.StatusBadRequest, body: `{"errors":[{"field":"body","code":"missing_field"}]}`, branch: freshdesk.AddNoteBranchProviderRejected},
		{name: "bad gateway", status: http.StatusBadGateway, branch: freshdesk.AddNoteBranchUncertain},
		{name: "another ticket", status: http.StatusCreated, body: `{"id":4,"ticket_id":999,"created_at":"2026-01-27T10:00:00Z"}`, branch: freshdesk.AddNoteBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newFreshdeskDexContext("note-"+test.name), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestAddNoteRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []freshdesk.AddNoteInput{{Body: "Hello"}, {TicketID: 3, Body: "  "}} {
		result, err := sdkgo.RunMutation(newFreshdeskDexContext("invalid-note"), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
		require.NoError(t, err)
		require.Equal(t, freshdesk.AddNoteBranchDefect, result.Branch)
	}
}
