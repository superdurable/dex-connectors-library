// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListTicketsReadsCursorPageAndProtectsProviderFields(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, "/api/v2/tickets", request.URL.Path)
		require.Equal(t, "20", request.URL.Query().Get("page[size]"))
		require.Equal(t, "cursor-1", request.URL.Query().Get("page[after]"))
		ticket := ticketJSON(35436, "open", []string{"billing"})
		ticket["custom_fields"] = []any{map[string]any{"id": 1, "value": "SENTINEL private"}}
		encoded, err := json.Marshal(map[string]any{"tickets": []any{ticket}, "meta": map[string]any{"has_more": true, "after_cursor": "cursor-2"}})
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	result, err := sdkgo.RunQuery(newZendeskDexContext("list-tickets"), newZendeskClient(t, provider.URL).ListTickets(), zendeskConnection, support.ListTicketsInput{PageSize: 20, Cursor: "cursor-1"})
	require.NoError(t, err)
	require.Equal(t, support.ListTicketsBranchListed, result.Branch)
	require.Equal(t, "cursor-2", result.Value.NextCursor)
	require.Len(t, result.Value.Tickets, 1)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}

func TestDeleteTicketAcceptsNoContentAndRetriesServerFailure(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusBadGateway} {
		provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			require.Equal(t, http.MethodDelete, request.Method)
			require.Equal(t, "/api/v2/tickets/35436", request.URL.Path)
			response.WriteHeader(status)
		})
		result, err := sdkgo.RunMutation(newZendeskDexContext("delete-ticket"), newZendeskClient(t, provider.URL).DeleteTicket(), zendeskConnection, support.DeleteTicketInput{TicketID: 35436})
		switch status {
		case http.StatusNoContent:
			require.NoError(t, err)
			require.Equal(t, support.DeleteTicketBranchDeleted, result.Branch)
		case http.StatusNotFound:
			require.NoError(t, err)
			require.Equal(t, support.DeleteTicketBranchNotFound, result.Branch)
		default:
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
	}
}

func TestRecoverSuspendedTicketClassifiesProviderResponses(t *testing.T) {
	for _, scenario := range []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{
		{http.StatusOK, support.RecoverSuspendedTicketBranchRecovered, false},
		{http.StatusNotFound, support.RecoverSuspendedTicketBranchNotFound, false},
		{http.StatusUnprocessableEntity, support.RecoverSuspendedTicketBranchProviderRejected, false},
		{http.StatusTooManyRequests, "", true},
		{http.StatusServiceUnavailable, "", true},
		{http.StatusBadGateway, "", true},
	} {
		provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			require.Equal(t, http.MethodPut, request.Method)
			require.Equal(t, "/api/v2/suspended_tickets/42/recover", request.URL.Path)
			if scenario.status == http.StatusOK {
				encoded, err := json.Marshal(map[string]any{"ticket": ticketJSON(35436, "open", []string{"recovered"})})
				require.NoError(t, err)
				writeJSON(t, response, scenario.status, string(encoded))
				return
			}
			response.WriteHeader(scenario.status)
		})
		result, err := sdkgo.RunMutation(newZendeskDexContext("recover-ticket"), newZendeskClient(t, provider.URL).RecoverSuspendedTicket(), zendeskConnection, support.RecoverSuspendedTicketInput{SuspendedTicketID: 42})
		if scenario.retry {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		} else {
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
		}
	}
}
