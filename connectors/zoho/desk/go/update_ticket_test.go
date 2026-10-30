// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateTicketReadsThenPatchesOnlyTheFieldsThatDiffer(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "On Hold", "On Hold"))
			return
		}
		var change map[string]any
		require.NoError(t, json.Unmarshal(mustReadBody(t, request), &change))
		value := ticketJSON(testTicketID, "Open", "Open")
		value["priority"] = change["priority"]
		writeValue(t, response, http.StatusOK, value)
	})
	result, err := sdkgo.RunMutation(newDeskDexContext("update"), newDeskClient(t, provider.URL).UpdateTicket(), deskConnection, desk.UpdateTicketInput{
		TicketID: testTicketID, Status: desk.TicketStatusOpen, Priority: "Urgent", AssigneeID: testAgentID,
	})
	require.NoError(t, err)
	require.Equal(t, desk.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, desk.TicketPriority("Urgent"), result.Value.Ticket.Priority, "a custom priority passes through")
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPatch, provider.request(1).method)
	require.Equal(t, "/api/v1/tickets/"+testTicketID, provider.request(1).path)
	require.JSONEq(t, `{"status":"Open","priority":"Urgent"}`, provider.request(1).body, "the assignee already matches, so it is not sent")
}

func TestUpdateTicketWritesNothingWhenEveryValueIsAlreadyApplied(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method, "nothing is written")
		writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
	})
	result, err := sdkgo.RunMutation(newDeskDexContext("update-applied"), newDeskClient(t, provider.URL).UpdateTicket(), deskConnection, desk.UpdateTicketInput{
		TicketID: testTicketID, Status: "open", Priority: desk.TicketPriorityHigh, AssigneeID: testAgentID, DepartmentID: testDepartmentID,
	})
	require.NoError(t, err)
	require.Equal(t, desk.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied, "status names compare without regard to letter case")
	require.Equal(t, 1, provider.requestCount())
}

func TestUpdateTicketKeepsTheAssigneeWhenItMovesTheDepartment(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
			return
		}
		value := ticketJSON(testTicketID, "Open", "Open")
		value["departmentId"] = "1892000000082069"
		writeValue(t, response, http.StatusOK, value)
	})
	result, err := sdkgo.RunMutation(newDeskDexContext("update-move"), newDeskClient(t, provider.URL).UpdateTicket(), deskConnection, desk.UpdateTicketInput{
		TicketID: testTicketID, DepartmentID: "1892000000082069", AssigneeID: testAgentID,
	})
	require.NoError(t, err)
	require.Equal(t, "1892000000082069", result.Value.Ticket.DepartmentID)
	require.JSONEq(t, `{"departmentId":"1892000000082069","assigneeId":"`+testAgentID+`"}`, provider.request(1).body)
}

func TestUpdateTicketRetriesEveryUnconfirmedWriteBecauseItIsSafeToRepeat(t *testing.T) {
	provider := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "On Hold", "On Hold"))
		case index == 1:
			writeJSON(t, response, http.StatusServiceUnavailable, `{"message":"SENTINEL"}`)
		default:
			dropConnection(t, response)
		}
	})
	client := newDeskClient(t, provider.URL)
	input := desk.UpdateTicketInput{TicketID: testTicketID, Status: desk.TicketStatusOpen}
	_, err := sdkgo.RunMutation(newDeskDexContext("update-outage"), client.UpdateTicket(), deskConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	_, err = sdkgo.RunMutation(newDeskDexContext("update-lost"), client.UpdateTicket(), deskConnection, input)
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestUpdateTicketSelectsNotFoundRejectedAndInvalidResponse(t *testing.T) {
	missing := newRecordingDesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusNotFound, `{"errorCode":"URL_NOT_FOUND"}`)
	})
	result, err := sdkgo.RunMutation(newDeskDexContext("update-missing"), newDeskClient(t, missing.URL).UpdateTicket(), deskConnection,
		desk.UpdateTicketInput{TicketID: testTicketID, Status: desk.TicketStatusClosed})
	require.NoError(t, err)
	require.Equal(t, desk.UpdateTicketBranchNotFound, result.Branch)

	blueprint := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
			return
		}
		writeJSON(t, response, http.StatusUnprocessableEntity, `{"errorCode":"UNPROCESSABLE_ENTITY","message":"SENTINEL blueprint"}`)
	})
	result, err = sdkgo.RunMutation(newDeskDexContext("update-rejected"), newDeskClient(t, blueprint.URL).UpdateTicket(), deskConnection,
		desk.UpdateTicketInput{TicketID: testTicketID, Status: desk.TicketStatusClosed})
	require.NoError(t, err)
	require.Equal(t, desk.UpdateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, "Zoho Desk rejected the request (HTTP 422) [UNPROCESSABLE_ENTITY]", result.Failure.Message)
	require.Equal(t, desk.TicketStatusOpen, result.Value.Ticket.Status, "the rejection carries the ticket as read")
	requireNoSentinel(t, result)

	malformed := newRecordingDesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(testTicketID, "Open", "Open"))
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"`+testTicketID+`"}`)
	})
	result, err = sdkgo.RunMutation(newDeskDexContext("update-malformed"), newDeskClient(t, malformed.URL).UpdateTicket(), deskConnection,
		desk.UpdateTicketInput{TicketID: testTicketID, Status: desk.TicketStatusClosed})
	require.NoError(t, err)
	require.Equal(t, desk.UpdateTicketBranchInvalidResponse, result.Branch, "the change may have been applied")
}

func TestUpdateTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingDesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]desk.UpdateTicketInput{
		"no change":        {TicketID: testTicketID},
		"missing ticket":   {Status: desk.TicketStatusOpen},
		"wildcard status":  {TicketID: testTicketID, Status: "Clos*"},
		"assignee e-mail":  {TicketID: testTicketID, AssigneeID: "jade@example.com"},
		"department label": {TicketID: testTicketID, DepartmentID: "Billing"},
	} {
		result, err := sdkgo.RunMutation(newDeskDexContext("update-defect"), newDeskClient(t, provider.URL).UpdateTicket(), deskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, desk.UpdateTicketBranchDefect, result.Branch, name)
	}
}

func mustReadBody(t *testing.T, request *http.Request) []byte {
	t.Helper()
	var body json.RawMessage
	require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
	return body
}
