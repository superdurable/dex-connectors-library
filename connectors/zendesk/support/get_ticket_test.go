// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTicketReturnsTheRequesterAndNewestCommentsFirst(t *testing.T) {
	longBody := strings.Repeat("é", support.MaxTextBytes)
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			ticket := ticketJSON(5512, "open", []string{"billing", "refund"})
			ticket["due_at"] = "2026-01-29T14:02:00Z"
			encoded, err := json.Marshal(map[string]any{"ticket": ticket, "users": []any{
				map[string]any{"id": 235323, "name": "Agent Seven", "email": "agent7@acme.example.com", "role": "agent"},
				map[string]any{"id": 20978392, "name": "Jane Smith", "email": "jane@acme.example.com", "role": "end-user", "phone": "SENTINEL phone"},
			}})
			require.NoError(t, err)
			writeJSON(t, response, http.StatusOK, string(encoded))
			return
		}
		encoded, err := json.Marshal(map[string]any{"comments": []any{
			map[string]any{"id": 1, "author_id": 20978392, "public": true, "body": "raw", "plain_body": "I was charged twice.", "created_at": "2026-01-26T14:02:00Z",
				"metadata": map[string]any{"system": map[string]any{"ip_address": "SENTINEL ip"}}},
			map[string]any{"id": 3, "author_id": 235323, "public": false, "body": longBody, "created_at": "2026-01-27T10:05:00Z"},
			map[string]any{"id": 2, "author_id": 235323, "public": true, "plain_body": "We're looking into it.", "created_at": "2026-01-27T10:00:00Z"},
		}, "meta": map[string]any{"has_more": true}})
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("get-ticket"), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection,
		support.GetTicketInput{TicketID: 5512, LatestCommentLimit: 3})
	require.NoError(t, err)
	require.Equal(t, support.GetTicketBranchFound, result.Branch)
	details := result.Value
	require.Equal(t, support.Ticket{
		ID: 5512, AgentURL: "https://acme.zendesk.com/agent/tickets/5512", Subject: "Double charge on order 88213",
		Description: "I was charged twice.", Status: support.TicketStatusOpen, CustomStatusID: 123,
		Priority: support.TicketPriorityHigh, Type: support.TicketTypeProblem, RequesterID: 20978392, SubmitterID: 20978392,
		AssigneeID: 235323, GroupID: 98738, OrganizationID: 509974, BrandID: 1234, Tags: []string{"billing", "refund"},
		ExternalID: "ERP-88213", Channel: "email",
		CreatedAt: time.Date(2026, 1, 26, 14, 2, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 1, 28, 8, 45, 0, 0, time.UTC),
		DueAt: timePointer(time.Date(2026, 1, 29, 14, 2, 0, 0, time.UTC)),
	}, details.Ticket)
	require.Equal(t, &support.TicketUser{ID: 20978392, Name: "Jane Smith", Email: "jane@acme.example.com", Role: "end-user"}, details.Requester)
	require.True(t, details.HasOlderComments)
	require.Len(t, details.LatestComments, 3)
	require.Equal(t, []int64{3, 2, 1}, []int64{details.LatestComments[0].ID, details.LatestComments[1].ID, details.LatestComments[2].ID})
	require.False(t, details.LatestComments[0].IsPublic)
	require.True(t, details.LatestComments[0].IsBodyTruncated)
	require.LessOrEqual(t, len(details.LatestComments[0].Body), support.MaxTextBytes)
	require.True(t, utf8.ValidString(details.LatestComments[0].Body))
	require.Equal(t, "I was charged twice.", details.LatestComments[2].Body, "plain_body is preferred over body")
	require.Equal(t, "5512", result.Receipt.ProviderObjectID)

	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, "/api/v2/tickets/5512", provider.request(0).path)
	require.Equal(t, []string{"users"}, provider.request(0).query["include"])
	require.Equal(t, "/api/v2/tickets/5512/comments", provider.request(1).path)
	require.Equal(t, []string{"-created_at"}, provider.request(1).query["sort"])
	require.Equal(t, []string{"3"}, provider.request(1).query["page[size]"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}

func TestGetTicketUsesTheDefaultCommentLimitAndReportsMissingTickets(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(5512, "solved", nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[],"meta":{"has_more":false}}`)
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("get-default"), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Equal(t, support.GetTicketBranchFound, result.Branch)
	require.Nil(t, result.Value.Requester, "a response without the requester sideload leaves it nil")
	require.NotNil(t, result.Value.LatestComments)
	require.Equal(t, []string{"5"}, provider.request(1).query["page[size]"])

	missing := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"error":"RecordNotFound","description":"Not found"}`)
	})
	result, err = sdkgo.RunQuery(newZendeskDexContext("get-missing"), newZendeskClient(t, missing.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 9999})
	require.NoError(t, err)
	require.Equal(t, support.GetTicketBranchNotFound, result.Branch)
	require.Equal(t, 1, missing.requestCount(), "comments are not read for a missing ticket")
}

func TestGetTicketRejectsInvalidInputAndMismatchedResponses(t *testing.T) {
	for name, input := range map[string]support.GetTicketInput{
		"zero ticket":     {},
		"negative limit":  {TicketID: 1, LatestCommentLimit: -1},
		"limit too large": {TicketID: 1, LatestCommentLimit: support.MaxLatestCommentLimit + 1},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			result, err := sdkgo.RunQuery(newZendeskDexContext("get-defect-"+name), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, input)
			require.NoError(t, err)
			require.Equal(t, support.GetTicketBranchDefect, result.Branch)
		})
	}
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, ticketEnvelopeJSON(7777, "open", nil))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"comments":[{"id":1,"created_at":"2026-01-01T00:00:00Z"},{"id":2,"created_at":"2026-01-01T00:00:00Z"}]}`)
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("get-mismatch"), newZendeskClient(t, provider.URL).GetTicket(), zendeskConnection, support.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Equal(t, support.GetTicketBranchInvalidResponse, result.Branch)
	require.Contains(t, result.Failure.Message, "another ticket")
}

func timePointer(value time.Time) *time.Time { return &value }
