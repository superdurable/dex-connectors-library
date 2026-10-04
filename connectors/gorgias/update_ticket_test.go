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

func TestUpdateTicketWritesOnlyWhatDiffersAndAddsAndRemovesTagsAsSets(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.Method {
		case http.MethodGet:
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "closed", []string{"billing", "stale"}, nil))
		case http.MethodPost:
			writeJSON(t, response, http.StatusCreated, ``)
		case http.MethodDelete:
			response.WriteHeader(http.StatusNoContent)
		default:
			updated := ticketJSON(5512, "open", []string{"billing", "dex-repeat-contact"}, nil)
			updated["priority"] = "high"
			writeValue(t, response, http.StatusAccepted, updated)
		}
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("update"), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection, gorgias.UpdateTicketInput{
		TicketID: 5512, Status: gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, AssigneeUserID: 7, AssigneeTeamID: 8,
		AddTags: []string{"billing", "dex-repeat-contact"}, RemoveTags: []string{"stale", "absent"},
	})
	require.NoError(t, err)
	require.Equal(t, gorgias.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 4, provider.requestCount())
	require.Equal(t, "/api/tickets/5512/tags", provider.request(1).path)
	require.JSONEq(t, `{"names":["dex-repeat-contact"]}`, provider.request(1).body, "a tag already present is not added again")
	require.Equal(t, http.MethodDelete, provider.request(2).method)
	require.JSONEq(t, `{"names":["stale"]}`, provider.request(2).body, "an absent tag is not removed")
	require.Equal(t, http.MethodPut, provider.request(3).method)
	require.JSONEq(t, `{"status":"open","priority":"high","assignee_team":{"id":8}}`, provider.request(3).body, "the assignee already matched")
	require.Equal(t, gorgias.TicketStatusOpen, result.Value.Ticket.Status)
	require.Equal(t, []string{"billing", "dex-repeat-contact"}, result.Value.Ticket.Tags)
}

func TestUpdateTicketThatAlreadyHoldsTheValuesWritesNothing(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing"}, nil))
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("applied"), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection, gorgias.UpdateTicketInput{
		TicketID: 5512, Status: gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityNormal, AssigneeUserID: 7, AddTags: []string{"billing"},
	})
	require.NoError(t, err)
	require.Equal(t, gorgias.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 1, provider.requestCount())
}

func TestUpdateTicketWithOnlyTagChangesReadsTheTicketBack(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			writeJSON(t, response, http.StatusCreated, ``)
		case index == 0:
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing"}, nil))
		default:
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing", "vip"}, nil))
		}
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("tags"), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection,
		gorgias.UpdateTicketInput{TicketID: 5512, AddTags: []string{"vip"}})
	require.NoError(t, err)
	require.Equal(t, gorgias.UpdateTicketBranchUpdated, result.Branch)
	require.Equal(t, []string{"billing", "vip"}, result.Value.Ticket.Tags)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method)
}

func TestUpdateTicketRejectedAfterATagChangeReturnsTheTicketAsReadAgain(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			writeJSON(t, response, http.StatusCreated, ``)
		case request.Method == http.MethodPut:
			writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL","data":{"assignee_team":["SENTINEL deleted"]}}}`)
		case index == 0:
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing"}, nil))
		default:
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing", "vip"}, nil))
		}
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("partial"), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection,
		gorgias.UpdateTicketInput{TicketID: 5512, AssigneeTeamID: 99, AddTags: []string{"vip"}})
	require.NoError(t, err)
	require.Equal(t, gorgias.UpdateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, []string{"billing", "vip"}, result.Value.Ticket.Tags, "the tag added before the rejected change is shown")
	require.Equal(t, 4, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(3).method)
	require.NotContains(t, result.Failure.Message, "SENTINEL")
}

func TestUpdateTicketBranches(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "missing", status: http.StatusNotFound, branch: gorgias.UpdateTicketBranchNotFound},
		{name: "rejected", status: http.StatusBadRequest, branch: gorgias.UpdateTicketBranchProviderRejected},
		{name: "outage", status: http.StatusInternalServerError, isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, nil))
					return
				}
				writeJSON(t, response, test.status, `{"error":{"msg":"SENTINEL"}}`)
			})
			result, err := sdkgo.RunMutation(newGorgiasDexContext("update-"+test.name), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection,
				gorgias.UpdateTicketInput{TicketID: 5512, Status: gorgias.TicketStatusClosed})
			if test.isRetry {
				require.Error(t, err, "an absolute update is safe to repeat")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestUpdateTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]gorgias.UpdateTicketInput{
		"no ticket":        {Status: gorgias.TicketStatusOpen},
		"no change":        {TicketID: 1},
		"freshdesk status": {TicketID: 1, Status: "4"},
		"add and remove":   {TicketID: 1, AddTags: []string{"a"}, RemoveTags: []string{"a"}},
		"negative team":    {TicketID: 1, AssigneeTeamID: -1},
	} {
		result, err := sdkgo.RunMutation(newGorgiasDexContext("invalid"), newGorgiasClient(t, provider.URL).UpdateTicket(), gorgiasConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, gorgias.UpdateTicketBranchDefect, result.Branch, name)
	}
}
