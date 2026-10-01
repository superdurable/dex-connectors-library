// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func followUpInput() support.UpdateTicketInput {
	return support.UpdateTicketInput{
		TicketID: 5512, Status: support.TicketStatusOpen, Priority: support.TicketPriorityUrgent, AssigneeID: 777, GroupID: 98738,
		AddTags: []string{"escalated"}, RemoveTags: []string{"refund"},
		Comment: &support.TicketCommentInput{Body: "Customer called again.", IsInternalNote: true},
	}
}

func TestUpdateTicketWritesASafeUpdateCarryingTheStepKey(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", []string{"billing", "refund"}))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"audits":[{"id":9,"metadata":{"custom":{"dex_idempotency_key":"another-step"}}},{"id":8,"metadata":{"custom":{}}}]}`)
		default:
			updated := ticketJSON(5512, "open", []string{"billing", "escalated"})
			updated["priority"], updated["assignee_id"], updated["updated_at"] = "urgent", 777, "2026-01-28T09:00:00Z"
			writeJSON(t, response, http.StatusOK, `{"ticket":`+mustJSON(t, updated)+`,"audit":{"id":10}}`)
		}
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-ticket"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, support.TicketStatusOpen, result.Value.Ticket.Status)
	require.Equal(t, []string{"billing", "escalated"}, result.Value.Ticket.Tags)

	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, "/api/v2/tickets/5512", provider.request(0).path)
	require.Equal(t, "/api/v2/tickets/5512/audits", provider.request(1).path)
	require.Equal(t, []string{"desc"}, provider.request(1).query["sort_order"])
	write := provider.request(2)
	require.Equal(t, http.MethodPut, write.method)
	require.Equal(t, "/api/v2/tickets/5512", write.path)
	require.Empty(t, write.header.Get("Idempotency-Key"), "Zendesk documents Idempotency-Key only for ticket creation")
	require.JSONEq(t, `{"ticket":{
		"status":"open","priority":"urgent","assignee_id":777,"tags":["billing","escalated"],
		"comment":{"body":"Customer called again.","public":false},
		"metadata":{"dex_idempotency_key":"`+string(result.Receipt.IdempotencyKey)+`"},
		"safe_update":true,"updated_stamp":"2026-01-28T08:45:00Z"}}`, write.body,
		"group_id already holds 98738, so only changed fields are sent")
}

func TestUpdateTicketFindsItsEarlierCommentAndWritesNothing(t *testing.T) {
	dexContext := newZendeskDexContext("update-repeated")
	key := stepIdempotencyKey(t, dexContext)
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{"billing", "escalated"}))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"audits":[{"id":10,"metadata":{"custom":{"dex_idempotency_key":"`+key+`"}}}]}`)
	})
	result, err := sdkgo.RunMutation(dexContext, newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 2, provider.requestCount(), "no second comment is written")
}

func TestUpdateTicketFieldChangeAlreadyHeldWritesNothing(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{"billing", "escalated"}))
	})
	input := support.UpdateTicketInput{TicketID: 5512, Status: support.TicketStatusOpen, AddTags: []string{"escalated"}, RemoveTags: []string{"refund"}}
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-noop"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 1, provider.requestCount(), "a field-only change needs no audit read when the ticket already holds it")
}

func TestUpdateTicketRemovingEveryTagSendsAnEmptyTagList(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{"billing"}))
			return
		}
		writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", []string{}))
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-untag"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection,
		support.UpdateTicketInput{TicketID: 5512, RemoveTags: []string{"billing"}})
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchUpdated, result.Branch)
	var body struct {
		Ticket map[string]json.RawMessage `json:"ticket"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &body))
	require.JSONEq(t, `[]`, string(body.Ticket["tags"]))
	require.NotContains(t, body.Ticket, "comment")
}

func TestUpdateTicketConflictsWhenTheTicketChangedAfterExpectedUpdatedAt(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "open", nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
	})
	input := support.UpdateTicketInput{TicketID: 5512, Status: support.TicketStatusSolved, ExpectedUpdatedAt: "2026-01-28T08:00:00Z"}
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-conflict"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, input)
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchConflict, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "ticket was updated at 2026-01-28T08:45:00Z, after expectedUpdatedAt", result.Failure.Message)
	require.Equal(t, int64(5512), result.Value.Ticket.ID, "the conflict carries the ticket as read")
	require.Equal(t, 2, provider.requestCount())

	input.ExpectedUpdatedAt = "2026-01-28T00:45:00-08:00"
	result, err = sdkgo.RunMutation(newZendeskDexContext("update-expected-match"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, input)
	require.NoError(t, err)
	require.NotEqual(t, support.UpdateTicketBranchConflict, result.Branch, "the same instant in another offset matches")
}

func TestUpdateTicketRetriesASafeUpdateCollisionFromAFreshRead(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "pending", nil))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
		default:
			writeJSON(t, response, http.StatusConflict, `{"error":"UpdateConflict","description":"SENTINEL Safe Update prevented the update due to outdated ticket data."}`)
		}
	})
	_, err := sdkgo.RunMutation(newZendeskDexContext("update-collision"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureConflict, retry.Failure.Kind)
	require.Equal(t, "Zendesk could not complete the request yet (HTTP 409) [UpdateConflict]", retry.Failure.Message)
}

func TestUpdateTicketRejectionsCarryTheTicketAsRead(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "closed", nil))
		case 1:
			writeJSON(t, response, http.StatusOK, `{"audits":[]}`)
		default:
			writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":"RecordInvalid","description":"SENTINEL closed prevents ticket update","details":{"status":[{"type":"closed_prevents_ticket_update"}]}}`)
		}
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("update-closed"), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, "Zendesk rejected the request (HTTP 422) [RecordInvalid; details: status=closed_prevents_ticket_update]", result.Failure.Message)
	require.Equal(t, support.TicketStatusClosed, result.Value.Ticket.Status)

	missing := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
	})
	result, err = sdkgo.RunMutation(newZendeskDexContext("update-missing"), newZendeskClient(t, missing.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.Equal(t, support.UpdateTicketBranchNotFound, result.Branch)
	require.Equal(t, 1, missing.requestCount())
}

func TestUpdateTicketValidatesInputBeforeAnyRequest(t *testing.T) {
	for name, input := range map[string]support.UpdateTicketInput{
		"no ticket":             {Status: support.TicketStatusOpen},
		"no change":             {TicketID: 5512},
		"expectation only":      {TicketID: 5512, ExpectedUpdatedAt: "2026-01-28T08:45:00Z"},
		"unknown status":        {TicketID: 5512, Status: "resolved"},
		"unknown priority":      {TicketID: 5512, Priority: "p1"},
		"tag added and removed": {TicketID: 5512, AddTags: []string{"billing"}, RemoveTags: []string{"billing"}},
		"blank comment":         {TicketID: 5512, Comment: &support.TicketCommentInput{Body: "  "}},
		"naive expectation":     {TicketID: 5512, Status: support.TicketStatusOpen, ExpectedUpdatedAt: "2026-01-28 08:45"},
		"negative assignee":     {TicketID: 5512, AssigneeID: -3},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			result, err := sdkgo.RunMutation(newZendeskDexContext("update-defect-"+name), newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, input)
			require.NoError(t, err)
			require.Equal(t, support.UpdateTicketBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}

// stepIdempotencyKey returns the key a Mutation derives for context by running a Step against a recorder.
func stepIdempotencyKey(t *testing.T, dexContext *zendeskDexContext) string {
	t.Helper()
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"error":"RecordNotFound"}`)
	})
	result, err := sdkgo.RunMutation(dexContext, newZendeskClient(t, provider.URL).UpdateTicket(), zendeskConnection, followUpInput())
	require.NoError(t, err)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)
	return string(result.Receipt.IdempotencyKey)
}
