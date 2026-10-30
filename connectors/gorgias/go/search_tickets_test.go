// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func ticketPage(nextCursor any, tickets ...map[string]any) map[string]any {
	return map[string]any{"object": "list", "uri": "/api/tickets", "data": tickets,
		"meta": map[string]any{"prev_cursor": nil, "next_cursor": nextCursor, "total_resources": nil}}
}

func listedTicket(id int64, status string, priority string, tags []string, updated string) map[string]any {
	ticket := ticketJSON(id, status, tags, nil)
	delete(ticket, "messages")
	ticket["priority"], ticket["updated_datetime"], ticket["messages_count"] = priority, updated, 3
	ticket["excerpt"] = "SENTINEL excerpt"
	return ticket
}

func TestSearchTicketsResolvesTheRequesterAndFiltersThePage(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == "/api/customers" {
			writeValue(t, response, http.StatusOK, map[string]any{"data": []map[string]any{
				{"id": 3924, "email": "Jane@Acme.example.com", "name": "Jane"}, {"id": 1, "email": "jane@acme.example.com.au"},
			}, "meta": map[string]any{"next_cursor": nil}})
			return
		}
		writeValue(t, response, http.StatusOK, ticketPage("WyJuZXh0IiwgMjldPQ==",
			listedTicket(10, "open", "high", []string{"billing"}, "2026-01-28T09:00:00"),
			listedTicket(11, "closed", "high", []string{"billing"}, "2026-01-28T08:00:00"),
			listedTicket(12, "open", "low", []string{"Billing"}, "2026-01-28T07:00:00"),
			listedTicket(13, "open", "normal", []string{"vip", "billing"}, "2026-01-28T06:00:00"),
		))
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("search"), newGorgiasClient(t, provider.URL).SearchTickets(), gorgiasConnection, gorgias.SearchTicketsInput{
		RequesterEmail: "jane@acme.example.com", Statuses: []gorgias.TicketStatus{gorgias.TicketStatusOpen}, Tags: []string{"billing"},
		ViewID: 44, PageSize: 50, Cursor: "WyJuZXh0IiwgMV0=",
	})
	require.NoError(t, err)
	require.Equal(t, gorgias.SearchTicketsBranchSearched, result.Branch)
	require.Equal(t, map[string][]string{"email": {"jane@acme.example.com"}, "limit": {"10"}}, provider.request(0).query)
	require.Equal(t, map[string][]string{
		"customer_id": {"3924"}, "view_id": {"44"}, "order_by": {"updated_datetime:desc"}, "limit": {"50"}, "trashed": {"false"},
		"cursor": {"WyJuZXh0IiwgMV0="},
	}, provider.request(1).query)
	var ids []int64
	for _, ticket := range result.Value.Tickets {
		ids = append(ids, ticket.ID)
	}
	require.Equal(t, []int64{10, 13}, ids, "closed and differently cased tags are filtered out")
	require.Equal(t, 4, result.Value.ListedCount)
	require.Equal(t, "WyJuZXh0IiwgMjldPQ==", result.Value.NextCursor)
	ticket := result.Value.Tickets[0]
	require.Equal(t, gorgias.TicketPriorityHigh, ticket.Priority)
	require.Equal(t, int64(3924), ticket.RequesterID)
	require.Equal(t, "jane@acme.example.com", ticket.RequesterEmail)
	require.Equal(t, int64(7), ticket.AssigneeUserID)
	require.Equal(t, 3, ticket.MessageCount)
}

func TestSearchTicketsForAnUnknownCustomerReturnsAnEmptyPageWithoutListing(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":[],"meta":{"next_cursor":null}}`)
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("unknown"), newGorgiasClient(t, provider.URL).SearchTickets(), gorgiasConnection,
		gorgias.SearchTicketsInput{RequesterEmail: "nobody@example.com"})
	require.NoError(t, err)
	require.Equal(t, gorgias.SearchTicketsBranchSearched, result.Branch)
	require.Empty(t, result.Value.Tickets)
	require.Equal(t, 1, provider.requestCount())
}

func TestSearchTicketsUpdatedSinceEndsPagingAtTheFirstOlderTicket(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, ticketPage("WyJuZXh0Il0=",
			listedTicket(10, "open", "normal", nil, "2026-01-28T09:00:00"),
			listedTicket(11, "open", "normal", nil, "2026-01-20T08:00:00"),
		))
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("since"), newGorgiasClient(t, provider.URL).SearchTickets(), gorgiasConnection,
		gorgias.SearchTicketsInput{RequesterID: 3924, UpdatedSince: "2026-01-21T00:00:00Z"})
	require.NoError(t, err)
	require.Len(t, result.Value.Tickets, 1)
	require.Empty(t, result.Value.NextCursor, "every later ticket is older")
	require.Equal(t, "30", provider.request(0).query["limit"][0])
}

func TestSearchTicketsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]gorgias.SearchTicketsInput{
		"zendesk status":     {Statuses: []gorgias.TicketStatus{"solved"}},
		"freshdesk priority": {Priorities: []gorgias.TicketPriority{"3"}},
		"duplicate status":   {Statuses: []gorgias.TicketStatus{"open", "open"}},
		"both requesters":    {RequesterEmail: "jane@example.com", RequesterID: 1},
		"display address":    {RequesterEmail: "Jane <jane@example.com>"},
		"page size":          {PageSize: 101},
		"cursor injection":   {Cursor: "abc&limit=1000"},
		"offset-less since":  {UpdatedSince: "2026-01-21T00:00:00"},
		"blank tag":          {Tags: []string{" "}},
	} {
		result, err := sdkgo.RunQuery(newGorgiasDexContext("invalid"), newGorgiasClient(t, provider.URL).SearchTickets(), gorgiasConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, gorgias.SearchTicketsBranchDefect, result.Branch, name)
	}
}

func TestSearchTicketsInvalidPagesSelectInvalidResponse(t *testing.T) {
	for name, body := range map[string]string{
		"no data":        `{"meta":{}}`,
		"bad cursor":     `{"data":[],"meta":{"next_cursor":"a b"}}`,
		"missing status": `{"data":[{"id":1,"created_datetime":"2026-01-26T14:02:00"}]}`,
	} {
		provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newGorgiasDexContext("invalid-page"), newGorgiasClient(t, provider.URL).SearchTickets(), gorgiasConnection, gorgias.SearchTicketsInput{})
		require.NoError(t, err, name)
		require.Equal(t, gorgias.SearchTicketsBranchInvalidResponse, result.Branch, name)
	}
}
