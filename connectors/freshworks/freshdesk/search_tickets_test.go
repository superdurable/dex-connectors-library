// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBuildTicketFilterQueryUsesFreshdeskIntegersAndQuotedValues(t *testing.T) {
	query, err := freshdesk.BuildTicketFilterQuery(freshdesk.SearchTicketsInput{
		Statuses:   []freshdesk.TicketStatus{freshdesk.TicketStatusOpen, freshdesk.TicketStatusPending, 8},
		Priorities: []freshdesk.TicketPriority{freshdesk.TicketPriorityUrgent},
		Tags:       []string{"billing", "Double Charge"}, UpdatedSinceDate: "2026-01-21", RequesterEmail: "jane@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, "(status:2 OR status:3 OR status:8) AND priority:4 AND (tag:'billing' OR tag:'Double Charge') AND updated_at:>'2026-01-21'", query,
		"a custom status passes through and the requester is never part of the query")

	query, err = freshdesk.BuildTicketFilterQuery(freshdesk.SearchTicketsInput{Statuses: []freshdesk.TicketStatus{freshdesk.TicketStatusResolved}})
	require.NoError(t, err)
	require.Equal(t, "status:4", query)

	manyTags := make([]string, 40)
	for index := range manyTags {
		manyTags[index] = "tag-number-" + strings.Repeat("x", index%5) + string(rune('a'+index%26)) + string(rune('a'+index/26))
	}
	for name, input := range map[string]freshdesk.SearchTicketsInput{
		"no condition":      {RequesterEmail: "jane@example.com"},
		"status below 2":    {Statuses: []freshdesk.TicketStatus{1}},
		"duplicate status":  {Statuses: []freshdesk.TicketStatus{2, 2}},
		"priority above 4":  {Priorities: []freshdesk.TicketPriority{5}},
		"quote in tag":      {Tags: []string{"it's"}},
		"operator in tag":   {Tags: []string{"a' OR status:2 OR tag:'b"}},
		"duplicate tag":     {Tags: []string{"billing", "Billing"}},
		"instant not date":  {UpdatedSinceDate: "2026-01-21T00:00:00Z"},
		"impossible date":   {UpdatedSinceDate: "2026-02-30"},
		"query over limit":  {Tags: manyTags},
		"zero status value": {Statuses: []freshdesk.TicketStatus{0}},
	} {
		_, err := freshdesk.BuildTicketFilterQuery(input)
		require.Error(t, err, name)
	}
}

func TestSearchTicketsSendsTheQuotedQueryAndReportsPages(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"total": 61, "results": []any{ticketJSON(47, 2, nil), ticketJSON(57, 3, nil)}})
	})
	result, err := sdkgo.RunQuery(newFreshdeskDexContext("search"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, freshdesk.SearchTicketsInput{
		Statuses: []freshdesk.TicketStatus{freshdesk.TicketStatusOpen}, Tags: []string{"billing"}, Page: 2,
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.SearchTicketsBranchSearched, result.Branch)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, "/api/v2/search/tickets", request.path)
	require.Equal(t, []string{`"status:2 AND tag:'billing'"`}, request.query["query"])
	require.Equal(t, []string{"2"}, request.query["page"])
	require.Equal(t, 61, result.Value.TotalMatched)
	require.Equal(t, 3, result.Value.NextPage, "61 matches fill three pages of 30")
	require.Len(t, result.Value.Tickets, 2)
	require.Equal(t, freshdesk.TicketStatusPending, result.Value.Tickets[1].Status)
	require.Equal(t, freshdesk.TicketPriorityMedium, result.Value.Tickets[0].Priority)
	require.Empty(t, result.Value.Tickets[0].Description, "search pages omit descriptions")
}

func TestSearchTicketsStopsAtFreshdesksLastPage(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"total": 900, "results": []any{ticketJSON(47, 2, nil)}})
	})
	for page, nextPage := range map[int]int{9: 10, 10: 0} {
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("last-page"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, freshdesk.SearchTicketsInput{
			Statuses: []freshdesk.TicketStatus{freshdesk.TicketStatusOpen}, Page: page,
		})
		require.NoError(t, err)
		require.Equal(t, nextPage, result.Value.NextPage, "page %d", page)
	}
}

func TestSearchTicketsResolvesTheRequesterEmailAndKeepsOnlyTheirTickets(t *testing.T) {
	const requesterID, otherRequesterID = int64(6007738334), int64(6007738335)
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.URL.Path == "/api/v2/contacts" {
			writeValue(t, response, http.StatusOK, []any{
				map[string]any{"id": requesterID, "name": "Jane", "email": "Jane@Acme.example.com"},
				map[string]any{"id": otherRequesterID, "name": "Jane AU", "email": "jane@acme.example.com.au"},
			})
			return
		}
		mine, theirs := ticketJSON(47, 2, nil), ticketJSON(57, 2, nil)
		theirs["requester_id"] = otherRequesterID
		writeValue(t, response, http.StatusOK, map[string]any{"total": 2, "results": []any{mine, theirs}})
	})
	result, err := sdkgo.RunQuery(newFreshdeskDexContext("requester"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, freshdesk.SearchTicketsInput{
		Tags: []string{"billing"}, RequesterEmail: "jane@acme.example.com",
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.SearchTicketsBranchSearched, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, "/api/v2/contacts", provider.request(0).path)
	require.Equal(t, []string{"jane@acme.example.com"}, provider.request(0).query["email"])
	require.Equal(t, []string{`"tag:'billing'"`}, provider.request(1).query["query"])
	require.Len(t, result.Value.Tickets, 1, "a look-alike address is another contact")
	require.Equal(t, int64(47), result.Value.Tickets[0].ID)
	require.Equal(t, 2, result.Value.TotalMatched, "the total counts tickets before the requester filter")
}

func TestSearchTicketsForAnUnknownRequesterReturnsNothingWithoutSearching(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `[]`)
	})
	result, err := sdkgo.RunQuery(newFreshdeskDexContext("unknown-requester"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, freshdesk.SearchTicketsInput{
		Tags: []string{"billing"}, RequesterEmail: "new@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.SearchTicketsBranchSearched, result.Branch)
	require.Empty(t, result.Value.Tickets)
	require.NotNil(t, result.Value.Tickets)
	require.Equal(t, 1, provider.requestCount())
}

func TestSearchTicketsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]freshdesk.SearchTicketsInput{
		"page above 10":   {Tags: []string{"billing"}, Page: 11},
		"negative page":   {Tags: []string{"billing"}, Page: -1},
		"display address": {Tags: []string{"billing"}, RequesterEmail: "Jane <jane@example.com>"},
		"no condition":    {},
	} {
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("invalid-search"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, input)
		require.NoError(t, err)
		require.Equal(t, freshdesk.SearchTicketsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}

func TestSearchTicketsRejectsInvalidPages(t *testing.T) {
	thirtyOne := make([]any, 31)
	for index := range thirtyOne {
		thirtyOne[index] = ticketJSON(int64(index+1), 2, nil)
	}
	for name, body := range map[string]any{
		"missing total":   map[string]any{"results": []any{}},
		"missing results": map[string]any{"total": 0},
		"oversized page":  map[string]any{"total": 31, "results": thirtyOne},
		"invalid ticket":  map[string]any{"total": 1, "results": []any{map[string]any{"id": 1}}},
	} {
		provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeValue(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newFreshdeskDexContext("invalid-page"), newFreshdeskClient(t, provider.URL).SearchTickets(), freshdeskConnection, freshdesk.SearchTicketsInput{Tags: []string{"billing"}})
		require.NoError(t, err)
		require.Equal(t, freshdesk.SearchTicketsBranchInvalidResponse, result.Branch, name)
	}
}
