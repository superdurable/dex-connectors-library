// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBuildTicketSearchQueryCombinesTypedFilters(t *testing.T) {
	query, err := support.BuildTicketSearchQuery(support.SearchTicketsInput{
		Statuses:       []support.TicketStatus{support.TicketStatusOpen, support.TicketStatusPending},
		RequesterEmail: "jane@acme.example.com", Tags: []string{"billing", "refund"},
		UpdatedSince: "2026-01-21T08:00:00-08:00", Text: "  double   charge ",
	})
	require.NoError(t, err)
	require.Equal(t, "status:open status:pending requester:jane@acme.example.com tags:billing tags:refund updated>=2026-01-21T16:00:00Z double charge", query)
	require.NotContains(t, query, "type:", "export search rejects a type in the query string")
}

func TestBuildTicketSearchQueryRejectsOperatorsAndInvalidFilters(t *testing.T) {
	for name, test := range map[string]struct {
		input   support.SearchTicketsInput
		message string
	}{
		"no filter":          {input: support.SearchTicketsInput{PageSize: 10}, message: "at least one of"},
		"unknown status":     {input: support.SearchTicketsInput{Statuses: []support.TicketStatus{"resolved"}}, message: "not a Zendesk status"},
		"duplicate status":   {input: support.SearchTicketsInput{Statuses: []support.TicketStatus{"open", "open"}}, message: "twice"},
		"display address":    {input: support.SearchTicketsInput{RequesterEmail: "Jane <jane@acme.example.com>"}, message: "bare email"},
		"operator in email":  {input: support.SearchTicketsInput{RequesterEmail: "jane@acme.example.com status:solved"}, message: "bare email"},
		"uppercase tag":      {input: support.SearchTicketsInput{Tags: []string{"Billing"}}, message: "lowercase"},
		"tag with space":     {input: support.SearchTicketsInput{Tags: []string{"a b"}}, message: "lowercase"},
		"negated tag":        {input: support.SearchTicketsInput{Tags: []string{"-billing"}}, message: "lowercase"},
		"naive time":         {input: support.SearchTicketsInput{UpdatedSince: "2026-01-21T00:00:00"}, message: "explicit offset"},
		"property in text":   {input: support.SearchTicketsInput{Text: "printer status:solved"}, message: "search operator"},
		"comparison in text": {input: support.SearchTicketsInput{Text: "updated>2020-01-01"}, message: "search operator"},
		"quote in text":      {input: support.SearchTicketsInput{Text: `"fire"`}, message: "search operator"},
		"exclusion in text":  {input: support.SearchTicketsInput{Text: "-spam"}, message: "search operator"},
		"too many words":     {input: support.SearchTicketsInput{Text: strings.Repeat("word ", 33)}, message: "more than 32"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := support.BuildTicketSearchQuery(test.input)
			require.ErrorContains(t, err, test.message)
		})
	}
}

func TestSearchTicketsReadsOneExportPageFilteredToTickets(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		first, second := ticketJSON(5512, "open", []string{"billing"}), ticketJSON(5601, "pending", []string{"billing"})
		first["result_type"], second["result_type"] = "ticket", "ticket"
		second["updated_at"] = "2026-01-29T10:00:00Z"
		encoded, err := json.Marshal(map[string]any{
			"results": []any{first, second}, "facets": nil,
			"meta":  map[string]any{"has_more": true, "after_cursor": "eyJmaWVsZCI6ImNyZWF0ZWRfYXQifQ=="},
			"links": map[string]any{"next": "https://acme.zendesk.com/api/v2/search/export?page[after]=x", "prev": nil},
		})
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	client := newZendeskClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newZendeskDexContext("search-page"), client.SearchTickets(), zendeskConnection, support.SearchTicketsInput{
		Statuses: []support.TicketStatus{support.TicketStatusOpen}, Tags: []string{"billing"}, PageSize: 2, Cursor: "previous-cursor",
	})
	require.NoError(t, err)
	require.Equal(t, support.SearchTicketsBranchSearched, result.Branch)
	require.Len(t, result.Value.Tickets, 2)
	require.Equal(t, int64(5512), result.Value.Tickets[0].ID)
	require.Equal(t, support.TicketStatusPending, result.Value.Tickets[1].Status)
	require.Empty(t, result.Value.Tickets[0].Description, "search pages omit descriptions")
	require.Equal(t, "eyJmaWVsZCI6ImNyZWF0ZWRfYXQifQ==", result.Value.NextCursor)
	require.Equal(t, "request-0001", result.Receipt.ProviderRequestID)

	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/api/v2/search/export", request.path)
	require.Equal(t, []string{"status:open tags:billing"}, request.query["query"])
	require.Equal(t, []string{"ticket"}, request.query["filter[type]"])
	require.Equal(t, []string{"2"}, request.query["page[size]"])
	require.Equal(t, []string{"previous-cursor"}, request.query["page[after]"])
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}

func TestSearchTicketsLastPageHasNoCursorAndDefaultPageSize(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"results":[],"meta":{"has_more":false,"after_cursor":null}}`)
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("search-last"), newZendeskClient(t, provider.URL).SearchTickets(), zendeskConnection,
		support.SearchTicketsInput{RequesterEmail: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, support.SearchTicketsBranchSearched, result.Branch)
	require.Empty(t, result.Value.Tickets)
	require.NotNil(t, result.Value.Tickets)
	require.Empty(t, result.Value.NextCursor)
	require.Equal(t, []string{"25"}, provider.request(0).query["page[size]"])
}

func TestSearchTicketsRejectsInvalidPagesAndInputs(t *testing.T) {
	for name, body := range map[string]string{
		"non-ticket result":    `{"results":[{"result_type":"user","id":1}],"meta":{"has_more":false}}`,
		"more without cursor":  `{"results":[],"meta":{"has_more":true}}`,
		"missing results":      `{"meta":{"has_more":false}}`,
		"ticket without dates": `{"results":[{"id":1,"status":"open"}],"meta":{"has_more":false}}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newZendeskDexContext("search-invalid-"+name), newZendeskClient(t, provider.URL).SearchTickets(), zendeskConnection,
				support.SearchTicketsInput{Tags: []string{"billing"}})
			require.NoError(t, err)
			require.Equal(t, support.SearchTicketsBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
		})
	}
	for name, input := range map[string]support.SearchTicketsInput{
		"page too large":    {Tags: []string{"billing"}, PageSize: support.MaxSearchPageSize + 1},
		"negative page":     {Tags: []string{"billing"}, PageSize: -1},
		"cursor with space": {Tags: []string{"billing"}, Cursor: "a b"},
		"no filters":        {},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingZendesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
			result, err := sdkgo.RunQuery(newZendeskDexContext("search-defect-"+name), newZendeskClient(t, provider.URL).SearchTickets(), zendeskConnection, input)
			require.NoError(t, err)
			require.Equal(t, support.SearchTicketsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}

func TestSearchTicketsExpiredCursorIsProviderRejected(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":"InvalidPaginationParameter","description":"SENTINEL cursor expired"}`)
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("search-expired"), newZendeskClient(t, provider.URL).SearchTickets(), zendeskConnection,
		support.SearchTicketsInput{Tags: []string{"billing"}, Cursor: "expired"})
	require.NoError(t, err)
	require.Equal(t, support.SearchTicketsBranchProviderRejected, result.Branch)
	require.Equal(t, "Zendesk rejected the request (HTTP 400) [InvalidPaginationParameter]", result.Failure.Message)
}
