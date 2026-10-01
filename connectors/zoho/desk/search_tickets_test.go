// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var searchNow = time.Date(2026, 1, 28, 9, 30, 0, 0, time.UTC)

func TestBuildTicketSearchParametersSendsTypedFiltersAsZohoDeskValues(t *testing.T) {
	parameters, err := desk.BuildTicketSearchParameters(desk.SearchTicketsInput{
		Statuses: []desk.TicketStatus{desk.TicketStatusOpen, "Waiting for Customer"}, Priorities: []desk.TicketPriority{desk.TicketPriorityHigh},
		ContactEmail: "jane@acme.example.com", DepartmentID: testDepartmentID, ModifiedSince: "2026-01-21T00:00:00+01:00", From: 50, Limit: 50,
	}, searchNow)
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"status": {"Open,Waiting for Customer"}, "priority": {"High"}, "email": {"jane@acme.example.com"}, "departmentId": {testDepartmentID},
		"modifiedTimeRange": {"2026-01-20T23:00:00.000Z,2026-01-28T09:30:00.000Z"}, "from": {"50"}, "limit": {"50"}, "sortBy": {"-modifiedTime"},
	}, parameters)

	parameters, err = desk.BuildTicketSearchParameters(desk.SearchTicketsInput{
		StatusTypes: []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold},
	}, searchNow)
	require.NoError(t, err)
	require.Equal(t, "${OPEN},${ONHOLD}", parameters.Get("status"))
	require.Equal(t, "0", parameters.Get("from"))
	require.Equal(t, "25", parameters.Get("limit"))

	parameters, err = desk.BuildTicketSearchParameters(desk.SearchTicketsInput{Statuses: []desk.TicketStatus{"Open"}, From: 4980, Limit: 30}, searchNow)
	require.NoError(t, err)
	require.Equal(t, "20", parameters.Get("limit"), "a page that would pass result 5000 ends there, so every NextFrom stays readable")
}

func TestBuildTicketSearchParametersRejectsValuesThatCouldChangeTheSearch(t *testing.T) {
	for name, input := range map[string]desk.SearchTicketsInput{
		"no filter":            {},
		"comma in status":      {Statuses: []desk.TicketStatus{"Open,Closed"}},
		"wildcard status":      {Statuses: []desk.TicketStatus{"Op*"}},
		"empty check":          {Priorities: []desk.TicketPriority{"${empty}"}},
		"padded priority":      {Priorities: []desk.TicketPriority{" High"}},
		"duplicate status":     {Statuses: []desk.TicketStatus{"Open", "open"}},
		"statuses and types":   {Statuses: []desk.TicketStatus{"Open"}, StatusTypes: []desk.TicketStatusType{desk.TicketStatusTypeOpen}},
		"unknown status type":  {StatusTypes: []desk.TicketStatusType{"Escalated"}},
		"wildcard email":       {ContactEmail: "jane*@acme.example.com"},
		"display address":      {ContactEmail: "Jane <jane@acme.example.com>"},
		"department name":      {DepartmentID: "Billing"},
		"date without offset":  {ModifiedSince: "2026-01-21T00:00:00"},
		"future modifiedSince": {ModifiedSince: "2026-02-01T00:00:00Z"},
		"negative from":        {Statuses: []desk.TicketStatus{"Open"}, From: -1},
		"from 5000":            {Statuses: []desk.TicketStatus{"Open"}, From: 5000},
		"limit above 100":      {Statuses: []desk.TicketStatus{"Open"}, Limit: 101},
		"negative limit":       {Statuses: []desk.TicketStatus{"Open"}, Limit: -1},
	} {
		_, err := desk.BuildTicketSearchParameters(input, searchNow)
		require.Error(t, err, name)
	}
}

func TestSearchTicketsKeepsOnlyTheContactsTicketsAndPagesByFromAndLimit(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		janes := ticketJSON("1892000000042034", "Open", "OPEN")
		lookalike := ticketJSON("1892000000042035", "Open", "Open")
		lookalike["email"] = "jane@acme.example.com.au"
		byContact := ticketJSON("1892000000042036", "On Hold", "ONHOLD")
		byContact["email"] = nil
		byContact["contact"] = map[string]any{"id": testContactID, "email": "JANE@acme.example.com", "lastName": "Smith"}
		writeValue(t, response, http.StatusOK, map[string]any{"data": []any{janes, lookalike, byContact}, "count": "7"})
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("search"), newDeskClient(t, provider.URL).SearchTickets(), deskConnection, desk.SearchTicketsInput{
		StatusTypes: []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold}, ContactEmail: "jane@acme.example.com", Limit: 3,
	})
	require.NoError(t, err)
	require.Equal(t, desk.SearchTicketsBranchSearched, result.Branch)
	require.Len(t, result.Value.Tickets, 2, "the look-alike address is dropped")
	require.Equal(t, "1892000000042034", result.Value.Tickets[0].ID)
	require.Equal(t, desk.TicketStatusTypeOpen, result.Value.Tickets[0].StatusType, "OPEN is reported as Open")
	require.Equal(t, desk.TicketStatusTypeOnHold, result.Value.Tickets[1].StatusType)
	require.Empty(t, result.Value.Tickets[0].DescriptionHTML, "search pages omit descriptions")
	require.Equal(t, 7, result.Value.TotalMatched)
	require.Equal(t, 3, result.Value.NextFrom, "a full page has a next page")
	request := provider.request(0)
	require.Equal(t, "/api/v1/tickets/search", request.path)
	require.Equal(t, []string{"${OPEN},${ONHOLD}"}, request.query["status"])
	requireNoSentinel(t, result)
}

func TestSearchTicketsTreatsNoContentAsAnEmptyLastPage(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.WriteHeader(http.StatusNoContent)
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("search-empty"), newDeskClient(t, provider.URL).SearchTickets(), deskConnection,
		desk.SearchTicketsInput{Priorities: []desk.TicketPriority{desk.TicketPriorityHigh}})
	require.NoError(t, err)
	require.Equal(t, desk.SearchTicketsBranchSearched, result.Branch)
	require.Equal(t, desk.SearchTicketsOutput{Tickets: []desk.Ticket{}}, result.Value)
}

func TestSearchTicketsStopsAtTheDeepestPageZohoDeskServes(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		data := []any{}
		for index := 0; index < 10; index++ {
			data = append(data, ticketJSON("189200000004"+string(rune('0'+index))+"000", "Open", "Open"))
		}
		writeValue(t, response, http.StatusOK, map[string]any{"data": data, "count": 9000})
	})
	result, err := sdkgo.RunQuery(newDeskDexContext("search-deepest"), newDeskClient(t, provider.URL).SearchTickets(), deskConnection,
		desk.SearchTicketsInput{Statuses: []desk.TicketStatus{"Open"}, From: 4990, Limit: 50})
	require.NoError(t, err)
	require.Equal(t, desk.SearchTicketsBranchSearched, result.Branch)
	require.Len(t, result.Value.Tickets, 10)
	require.Zero(t, result.Value.NextFrom, "Zoho Desk serves no result past 5000")
	require.Equal(t, []string{"10"}, provider.request(0).query["limit"])
}

func TestSearchTicketsRejectsAnInvalidPage(t *testing.T) {
	for name, body := range map[string]string{
		"no data":        `{"count":1}`,
		"not an object":  `[]`,
		"missing status": `{"data":[{"id":"1892000000042034","createdTime":"2026-01-26T14:02:00.000Z","modifiedTime":"2026-01-26T14:02:00.000Z"}],"count":1}`,
		"text ID":        `{"data":[{"id":"SENTINEL","status":"Open"}],"count":1}`,
		"oversized page": `{"data":[{},{}],"count":2}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newDeskDexContext("search-invalid"), newDeskClient(t, provider.URL).SearchTickets(), deskConnection,
				desk.SearchTicketsInput{Statuses: []desk.TicketStatus{"Open"}, Limit: 1})
			require.NoError(t, err)
			require.Equal(t, desk.SearchTicketsBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
			requireNoSentinel(t, result)
		})
	}
}

func TestSearchTicketsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	result, err := sdkgo.RunQuery(newDeskDexContext("search-defect"), newDeskClient(t, provider.URL).SearchTickets(), deskConnection,
		desk.SearchTicketsInput{ContactEmail: "jane*"})
	require.NoError(t, err)
	require.Equal(t, desk.SearchTicketsBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
}
