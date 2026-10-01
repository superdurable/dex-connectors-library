// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTicketReadsTheContactAndNewestThreadsAndComments(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/threads"):
			writeValue(t, response, http.StatusOK, map[string]any{"data": []any{
				map[string]any{"id": "1892000000413186", "channel": "EMAIL", "direction": "in", "visibility": "public", "status": "SUCCESS",
					"summary": "I was charged twice", "isDescriptionThread": true, "createdTime": "2026-01-26T14:02:00.000Z",
					"fromEmailAddress": "SENTINEL@example.com", "author": map[string]any{"name": "Jane Smith", "type": "END_USER", "email": "SENTINEL@example.com"}},
				map[string]any{"id": "1892000001004072", "channel": "EMAIL", "direction": "out", "visibility": "public", "status": "SUCCESS",
					"summary": "We are looking into it", "isDescriptionThread": false, "createdTime": "2026-01-27T10:05:00.000Z",
					"author": map[string]any{"name": "Jade Tywin", "type": "AGENT"}},
			}})
		case strings.HasSuffix(request.URL.Path, "/comments"):
			writeValue(t, response, http.StatusOK, map[string]any{"data": []any{
				map[string]any{"id": "4000000529001", "isPublic": false, "contentType": "html", "content": "<div>Refund issued, see REF-771.</div>",
					"plainText": "Refund issued, see REF-771.", "commenterId": testAgentID, "commentedTime": "2026-01-27T10:00:00.000Z", "modifiedTime": nil,
					"commenter": map[string]any{"name": "Jade Tywin", "type": "AGENT", "email": "SENTINEL@example.com"}},
			}})
		default:
			value := ticketJSON(testTicketID, "On Hold", "On Hold")
			value["contact"] = map[string]any{"id": testContactID, "firstName": "Jane", "lastName": "Smith", "email": "jane@acme.example.com", "phone": "SENTINEL"}
			writeValue(t, response, http.StatusOK, value)
		}
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("get"), newDeskClient(t, provider.URL).GetTicket(), deskConnection,
		desk.GetTicketInput{TicketID: testTicketID, ThreadLimit: 2, CommentLimit: 3})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchFound, result.Branch)
	details := result.Value
	require.Equal(t, testTicketID, details.Ticket.ID)
	require.Equal(t, desk.TicketStatus("On Hold"), details.Ticket.Status)
	require.Equal(t, desk.TicketStatusTypeOnHold, details.Ticket.StatusType)
	require.Equal(t, desk.TicketPriorityHigh, details.Ticket.Priority)
	require.Equal(t, "<div>I was charged twice.</div>", details.Ticket.DescriptionHTML)
	require.Equal(t, 2, details.Ticket.ThreadCount)
	require.Equal(t, time.Date(2026, 1, 28, 8, 45, 0, 0, time.UTC), details.Ticket.ModifiedAt)
	require.Equal(t, &desk.TicketContact{ID: testContactID, FirstName: "Jane", LastName: "Smith", Email: "jane@acme.example.com"}, details.Contact)
	require.Len(t, details.Threads, 2)
	require.Equal(t, "1892000001004072", details.Threads[0].ID, "threads are newest first")
	require.Equal(t, "Jane Smith", details.Threads[1].AuthorName)
	require.True(t, details.Threads[1].IsDescriptionThread)
	require.False(t, details.HasMoreThreads)
	require.Equal(t, []desk.TicketComment{{
		ID: "4000000529001", IsPublic: false, Content: "Refund issued, see REF-771.", ContentType: "plainText",
		CommenterID: testAgentID, CommenterName: "Jade Tywin", CommenterType: "AGENT", CommentedAt: time.Date(2026, 1, 27, 10, 0, 0, 0, time.UTC),
	}}, details.Comments)
	require.False(t, details.HasMoreComments)
	requireNoSentinel(t, result)

	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, "/api/v1/tickets/"+testTicketID, provider.request(0).path)
	require.Equal(t, []string{"contacts"}, provider.request(0).query["include"])
	require.Equal(t, map[string][]string{"from": {"0"}, "limit": {"2"}, "sortBy": {"-sendDateTime"}}, provider.request(1).query)
	require.Equal(t, map[string][]string{"from": {"0"}, "limit": {"3"}, "sortBy": {"-commentedTime"}}, provider.request(2).query)
}

func TestGetTicketBoundsTextAndReportsOlderThreadsAndComments(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/threads"):
			response.WriteHeader(http.StatusNoContent)
		case strings.HasSuffix(request.URL.Path, "/comments"):
			writeValue(t, response, http.StatusOK, map[string]any{"data": []any{map[string]any{
				"id": "4000000529007", "isPublic": "true", "contentType": "plainText", "content": strings.Repeat("é", desk.MaxTextBytes),
				"commenterId": testAgentID, "commentedTime": "2026-01-27T10:14:37.000Z", "modifiedTime": "2026-01-27T10:20:00.000Z",
			}}})
		default:
			value := ticketJSON(testTicketID, "Open", "Open")
			value["description"] = strings.Repeat("a", desk.MaxTextBytes+1)
			value["threadCount"], value["commentCount"] = "4", 9
			writeValue(t, response, http.StatusOK, value)
		}
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("get-bounded"), newDeskClient(t, provider.URL).GetTicket(), deskConnection,
		desk.GetTicketInput{TicketID: testTicketID, CommentLimit: 1})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchFound, result.Branch)
	require.True(t, result.Value.Ticket.IsDescriptionTruncated)
	require.Len(t, result.Value.Ticket.DescriptionHTML, desk.MaxTextBytes)
	require.Empty(t, result.Value.Threads, "204 is an empty thread list")
	require.True(t, result.Value.HasMoreThreads)
	require.Len(t, result.Value.Comments, 1)
	require.True(t, result.Value.Comments[0].IsPublic, `Zoho Desk's "true" string is a public comment`)
	require.True(t, result.Value.Comments[0].IsContentTruncated)
	require.LessOrEqual(t, len(result.Value.Comments[0].Content), desk.MaxTextBytes)
	require.NotNil(t, result.Value.Comments[0].ModifiedAt)
	require.True(t, result.Value.HasMoreComments)
	require.Equal(t, []string{"5"}, provider.request(1).query["limit"], "zero uses the default thread limit")
}

func TestGetTicketSelectsNotFoundAndInvalidResponse(t *testing.T) {
	missing := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND","message":"SENTINEL"}`)
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("get-missing"), newDeskClient(t, missing.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchNotFound, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)

	another := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, ticketBodyJSON("1892000000099999", "Open", "Open"))
	})
	result, err = sdkgo.RunQuery(newDeskDexContext("get-another"), newDeskClient(t, another.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchInvalidResponse, result.Branch)
	require.Contains(t, result.Failure.Message, "another ticket")

	badComments := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/threads"):
			writeJSON(t, response, http.StatusOK, `{"data":[]}`)
		case strings.HasSuffix(request.URL.Path, "/comments"):
			writeJSON(t, response, http.StatusOK, `{"data":[{"id":"4000000529001","commentedTime":"yesterday"}]}`)
		default:
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
		}
	})
	result, err = sdkgo.RunQuery(newDeskDexContext("get-bad-comments"), newDeskClient(t, badComments.URL).GetTicket(), deskConnection, desk.GetTicketInput{TicketID: testTicketID})
	require.NoError(t, err)
	require.Equal(t, desk.GetTicketBranchInvalidResponse, result.Branch)
	require.Contains(t, result.Failure.Message, "comment list")
}

func TestGetTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]desk.GetTicketInput{
		"missing ID":     {},
		"ticket number":  {TicketID: "#101"},
		"path traversal": {TicketID: "../organizations"},
		"thread limit":   {TicketID: testTicketID, ThreadLimit: desk.MaxThreadLimit + 1},
		"comment limit":  {TicketID: testTicketID, CommentLimit: -1},
	} {
		result, err := sdkgo.RunQuery(newDeskDexContext("get-defect"), newDeskClient(t, provider.URL).GetTicket(), deskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, desk.GetTicketBranchDefect, result.Branch, name)
	}
}
