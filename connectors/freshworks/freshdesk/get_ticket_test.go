// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTicketReadsTheRequesterAndTheFirstConversations(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/conversations") {
			response.Header().Set("Link", `<https://acme.freshdesk.com/api/v2/tickets/20/conversations?page=2&per_page=2>; rel="next"`)
			writeValue(t, response, http.StatusOK, []any{
				map[string]any{"id": 5, "body": "<div>Refund already issued.</div>", "body_text": "Refund already issued.", "private": true, "incoming": false,
					"source": 2, "user_id": 1, "ticket_id": 20, "created_at": "2026-01-27T10:05:00Z"},
				map[string]any{"id": 3, "body_text": strings.Repeat("é", freshdesk.MaxTextBytes), "private": false, "incoming": true,
					"source": 0, "user_id": 6007738334, "ticket_id": 20, "created_at": "2026-01-27T10:00:00Z"},
			})
			return
		}
		ticket := ticketJSON(20, 4, []string{"billing", "refund"})
		ticket["requester"] = map[string]any{"id": 6007738334, "name": "Jane Smith", "email": "jane@acme.example.com", "mobile": nil, "phone": "SENTINEL"}
		writeValue(t, response, http.StatusOK, ticket)
	})
	result, err := sdkgo.RunQuery(newFreshdeskDexContext("get"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 20, ConversationLimit: 2})
	require.NoError(t, err)
	require.Equal(t, freshdesk.GetTicketBranchFound, result.Branch)
	require.Equal(t, []string{"requester"}, provider.request(0).query["include"])
	require.Equal(t, "/api/v2/tickets/20/conversations", provider.request(1).path)
	require.Equal(t, []string{"2"}, provider.request(1).query["per_page"])
	require.Equal(t, []string{"1"}, provider.request(1).query["page"])

	details := result.Value
	require.Equal(t, freshdesk.TicketStatusResolved, details.Ticket.Status)
	require.Equal(t, "I was charged twice.", details.Ticket.Description)
	require.Equal(t, []string{"billing", "refund"}, details.Ticket.Tags)
	require.Equal(t, freshdesk.TicketSourceEmail, details.Ticket.Source)
	require.Equal(t, int64(6001263404), details.Ticket.ResponderID)
	require.Equal(t, time.Date(2026, 1, 29, 14, 2, 0, 0, time.UTC), *details.Ticket.DueBy)
	require.Nil(t, details.Ticket.FirstResponseDueBy)
	require.Equal(t, &freshdesk.TicketRequester{ID: 6007738334, Name: "Jane Smith", Email: "jane@acme.example.com"}, details.Requester)
	require.True(t, details.HasMoreConversations)
	require.Len(t, details.Conversations, 2)
	require.Equal(t, int64(3), details.Conversations[0].ID, "conversations are oldest first")
	require.Equal(t, freshdesk.ConversationSourceReply, details.Conversations[0].Source)
	require.True(t, details.Conversations[0].IsIncoming)
	require.True(t, details.Conversations[0].IsBodyTruncated)
	require.LessOrEqual(t, len(details.Conversations[0].Body), freshdesk.MaxTextBytes)
	require.True(t, strings.HasSuffix(details.Conversations[0].Body, "é"), "truncation keeps whole UTF-8 characters")
	require.Equal(t, "Refund already issued.", details.Conversations[1].Body)
	require.True(t, details.Conversations[1].IsPrivate)
	require.Equal(t, freshdesk.ConversationSourceNote, details.Conversations[1].Source)
}

func TestGetTicketWithoutALinkHeaderHasNoMoreConversations(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if strings.HasSuffix(request.URL.Path, "/conversations") {
			writeJSON(t, response, http.StatusOK, `[]`)
			return
		}
		writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 2, nil))
	})
	result, err := sdkgo.RunQuery(newFreshdeskDexContext("get-default"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 20})
	require.NoError(t, err)
	require.Equal(t, freshdesk.GetTicketBranchFound, result.Branch)
	require.False(t, result.Value.HasMoreConversations)
	require.NotNil(t, result.Value.Conversations)
	require.Nil(t, result.Value.Requester)
	require.Equal(t, []string{"10"}, provider.request(1).query["per_page"])
}

func TestGetTicketRejectsInconsistentResponses(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter, *http.Request){
		"another requester": func(response http.ResponseWriter, request *http.Request) {
			ticket := ticketJSON(20, 2, nil)
			ticket["requester"] = map[string]any{"id": 1, "email": "other@example.com"}
			writeValue(t, response, http.StatusOK, ticket)
		},
		"conversations larger than requested": func(response http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, "/conversations") {
				writeJSON(t, response, http.StatusOK, `[{"id":1,"created_at":"2026-01-27T10:00:00Z"},{"id":2,"created_at":"2026-01-27T10:00:00Z"}]`)
				return
			}
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 2, nil))
		},
		"conversation without ID": func(response http.ResponseWriter, request *http.Request) {
			if strings.HasSuffix(request.URL.Path, "/conversations") {
				writeJSON(t, response, http.StatusOK, `[{"created_at":"2026-01-27T10:00:00Z"}]`)
				return
			}
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 2, nil))
		},
	} {
		provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) { reply(response, request) })
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("inconsistent"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, freshdesk.GetTicketInput{TicketID: 20, ConversationLimit: 1})
		require.NoError(t, err)
		require.Equal(t, freshdesk.GetTicketBranchInvalidResponse, result.Branch, name)
	}
}

func TestGetTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []freshdesk.GetTicketInput{{}, {TicketID: -1}, {TicketID: 1, ConversationLimit: 21}, {TicketID: 1, ConversationLimit: -1}} {
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("invalid-get"), newFreshdeskClient(t, provider.URL).GetTicket(), freshdeskConnection, input)
		require.NoError(t, err)
		require.Equal(t, freshdesk.GetTicketBranchDefect, result.Branch)
	}
}
