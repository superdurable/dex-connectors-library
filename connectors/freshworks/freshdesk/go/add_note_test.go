// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"providerId":"5"`)
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
		// branch is empty for a Retry that keeps the claim, so the next attempt resends below the cap.
		branch    sdkgo.BranchID
		heartbeat string
		// readBacks counts conversation-list reads after an accepted conversation ID.
		readBacks int
	}{
		{name: "missing ticket", status: http.StatusNotFound, branch: freshdesk.AddNoteBranchNotFound, heartbeat: `"state":"canResend"`},
		{name: "rejected", status: http.StatusBadRequest, body: `{"errors":[{"field":"body","code":"missing_field"}]}`, branch: freshdesk.AddNoteBranchProviderRejected, heartbeat: `"state":"canResend"`},
		{name: "forbidden", status: http.StatusForbidden, branch: freshdesk.AddNoteBranchProviderRejected, heartbeat: `"state":"canResend"`},
		{name: "bad gateway", status: http.StatusBadGateway, heartbeat: `"state":"claimed"`},
		{name: "conflict", status: http.StatusConflict, heartbeat: `"state":"claimed"`},
		{name: "oversized refusal", status: http.StatusBadRequest, body: `{"errors":"` + strings.Repeat("x", 4096) + `"}`, heartbeat: `"state":"claimed"`},
		{name: "answer without a conversation ID", status: http.StatusCreated, body: `{"ticket_id":3}`, branch: freshdesk.AddNoteBranchUncertain, heartbeat: `"state":"claimed"`},
		{name: "another ticket", status: http.StatusCreated, body: `{"id":4,"ticket_id":999,"created_at":"2026-01-27T10:00:00Z"}`, branch: freshdesk.AddNoteBranchUncertain, heartbeat: `"providerId":"4"`, readBacks: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, `[]`)
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			ctx := newFreshdeskDexContext("note-" + test.name)
			client, err := freshdesk.New(freshdesk.Config{Domain: testDomain, MaxResponseBytes: 2048}, testCredentialProvider(), freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(ctx, client.AddNote(), freshdeskConnection, freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"})
			require.Equal(t, 1+test.readBacks, provider.requestCount())
			require.Contains(t, string(ctx.recordedHeartbeat), test.heartbeat)
			if test.branch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "an unknown outcome below the send cap is resent by the next attempt")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.readBacks != 0 {
				replayed, replayErr := sdkgo.RunMutation(ctx.nextAttempt(), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"})
				require.NoError(t, replayErr)
				require.Equal(t, freshdesk.AddNoteBranchUncertain, replayed.Branch)
				require.Equal(t, 2+test.readBacks, provider.requestCount())
				require.Equal(t, http.MethodGet, provider.request(provider.requestCount()-1).method, "a note whose ID Freshdesk returned is read back, never resent")
			}
		})
	}
}

func TestAddNoteResendsBelowTheCapAndSelectsUncertainAtTheCap(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	client := newFreshdeskClient(t, provider.URL)
	input := freshdesk.AddNoteInput{TicketID: 141, Body: "We are working on this issue.", IsPublicReply: true}
	attempt := newFreshdeskDexContext("reply-cap")
	for sendCount := 1; sendCount < 3; sendCount++ {
		_, err := sdkgo.RunMutation(attempt, client.AddNote(), freshdeskConnection, input)
		require.Error(t, err, "send %d leaves room under the cap of 3", sendCount)
		attempt = attempt.nextAttempt()
	}
	result, err := sdkgo.RunMutation(attempt, client.AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchUncertain, result.Branch, "the third send reaches the cap")
	result, err = sdkgo.RunMutation(attempt.nextAttempt(), client.AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 3, provider.requestCount(), "the fence refuses a fourth send")
}

func TestAddNoteRefusalAfterAnUnknownOutcomeSelectsUncertain(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error"}`)
			return
		}
		writeJSON(t, response, http.StatusNotFound, `{}`)
	})
	input := freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"}
	first := newFreshdeskDexContext("note-refusal-after-unknown")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.Error(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchUncertain, result.Branch, "the first send may have added the note")
	require.Equal(t, 2, provider.requestCount())
}

func TestAddNoteRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []freshdesk.AddNoteInput{{Body: "Hello"}, {TicketID: 3, Body: "  "}} {
		result, err := sdkgo.RunMutation(newFreshdeskDexContext("invalid-note"), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
		require.NoError(t, err)
		require.Equal(t, freshdesk.AddNoteBranchDefect, result.Branch)
	}
}

func TestAddNoteRetriesServiceUnavailableWithoutDuplicateReply(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			writeJSON(t, response, http.StatusServiceUnavailable, `{"code":"service_unavailable"}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, `[{"id":7,"ticket_id":141,"source":0,"private":false,"body_text":"We are working on this issue.","created_at":"2026-01-27T10:00:00Z"}]`)
		default:
			writeJSON(t, response, http.StatusCreated, `{"id":7,"ticket_id":141,"body_text":"We are working on this issue.","created_at":"2026-01-27T10:00:00Z"}`)
		}
	})
	input := freshdesk.AddNoteInput{TicketID: 141, Body: "We are working on this issue.", IsPublicReply: true}
	first := newFreshdeskDexContext("reply-503")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	second := first.nextAttempt()
	result, err := sdkgo.RunMutation(second, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Contains(t, string(second.recordedHeartbeat), `"providerId":"7"`)
	result, err = sdkgo.RunMutation(second.nextAttempt(), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchAdded, result.Branch, "the saved reply ID is read back, never resent")
	require.Equal(t, int64(7), result.Value.Conversation.ID)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method)
	require.Equal(t, "/api/v2/tickets/141/conversations", provider.request(2).path)
}

func TestAddNoteReadsASavedConversationIDBackAcrossConversationPages(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method != http.MethodGet:
			writeJSON(t, response, http.StatusCreated, `{"id":9003,"created_at":"SENTINEL"}`)
		case index == 1:
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error"}`)
		case request.URL.Query().Get("page") == "1":
			response.Header().Set("Link", `<https://acme.freshdesk.com/api/v2/tickets/3/conversations?page=2>; rel="next"`)
			writeJSON(t, response, http.StatusOK, `[{"id":9001,"ticket_id":3,"created_at":"2026-01-27T10:00:00Z"}]`)
		default:
			writeJSON(t, response, http.StatusOK, `[{"id":9003,"ticket_id":3,"source":2,"private":true,"body_text":"Hello","created_at":"2026-01-27T10:00:00Z"}]`)
		}
	})
	input := freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"}
	first := newFreshdeskDexContext("note-saved-id-pages")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a transient read-back failure retries because the conversation ID is saved")
	require.Contains(t, string(first.recordedHeartbeat), `"providerId":"9003"`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchAdded, result.Branch)
	require.Equal(t, "Hello", result.Value.Conversation.Body)
	require.True(t, result.Value.Conversation.IsPrivate)
	require.Equal(t, "9003", result.Receipt.ProviderObjectID)
	require.Equal(t, 4, provider.requestCount())
	require.Equal(t, "100", provider.request(2).query["per_page"][0])
	require.Equal(t, "2", provider.request(3).query["page"][0])
}

func TestAddNoteAcceptedConversationIDMissingFromTheListSelectsUncertain(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusNotFound, `{}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"id":9003,"created_at":"SENTINEL"}`)
	})
	ctx := newFreshdeskDexContext("note-accepted-id-missing")
	result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, freshdesk.AddNoteInput{TicketID: 3, Body: "Hello"})
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchUncertain, result.Branch, "a read-back 404 after acceptance never selects notFound")
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount())
}

func TestAddNoteReadsTheDispatchCheckpointBeforeResolvingCredentials(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":7,"ticket_id":141,"body_text":"We are working on this issue.","created_at":"2026-01-27T10:00:00Z"}`)
	})
	input := freshdesk.AddNoteInput{TicketID: 141, Body: "We are working on this issue.", IsPublicReply: true}
	first := newFreshdeskDexContext("reply-credential-rotation")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: projectconfig.ErrObjectNotFound},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AddNote(), freshdeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, freshdesk.AddNoteBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}
