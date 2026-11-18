// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateTicketSendsOnlyChangedFieldsAndTheCompleteTagList(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 3, []string{"billing", "VIP"}))
			return
		}
		writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 2, []string{"VIP", "dex-repeat-contact"}))
	})
	result, err := sdkgo.RunMutation(newFreshdeskDexContext("update"), newFreshdeskClient(t, provider.URL).UpdateTicket(), freshdeskConnection, freshdesk.UpdateTicketInput{
		TicketID: 20, Status: freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityMedium, GroupID: 156, ResponderID: 77,
		AddTags: []string{"dex-repeat-contact", "vip"}, RemoveTags: []string{"BILLING"},
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.UpdateTicketBranchUpdated, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 2, provider.requestCount())
	write := provider.request(1)
	require.Equal(t, http.MethodPut, write.method)
	require.Equal(t, "/api/v2/tickets/20", write.path)
	require.JSONEq(t, `{"status":2,"responder_id":77,"tags":["VIP","dex-repeat-contact"]}`, write.body,
		"priority and group already match, a tag present in another case is kept, and removal ignores case")
	require.Equal(t, freshdesk.TicketStatusOpen, result.Value.Ticket.Status)
}

func TestUpdateTicketThatAlreadyHoldsTheValuesWritesNothing(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method)
		writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 2, []string{"billing", "dex-repeat-contact"}))
	})
	result, err := sdkgo.RunMutation(newFreshdeskDexContext("update-applied"), newFreshdeskClient(t, provider.URL).UpdateTicket(), freshdeskConnection, freshdesk.UpdateTicketInput{
		TicketID: 20, Status: freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityMedium, AddTags: []string{"dex-repeat-contact"}, RemoveTags: []string{"refund"},
	})
	require.NoError(t, err)
	require.Equal(t, freshdesk.UpdateTicketBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 1, provider.requestCount())
}

func TestUpdateTicketRetriesEveryUnconfirmedWrite(t *testing.T) {
	for name, reply := range map[string]func(http.ResponseWriter){
		"server error": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"SENTINEL"}`)
		},
		"rate limit": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"code":"SENTINEL"}`)
		},
		"conflict": func(response http.ResponseWriter) { writeJSON(t, response, http.StatusConflict, `{}`) },
		"unlisted client error": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusMethodNotAllowed, `{}`)
		},
		"lost response":          func(response http.ResponseWriter) { dropConnection(t, response) },
		"invalid written ticket": func(response http.ResponseWriter) { writeJSON(t, response, http.StatusOK, `{"id":20}`) },
		"reflected key": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":20,"subject":"`+testAPIKey+`"}`)
		},
		"oversized written ticket": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"id":20,"description_text":"`+strings.Repeat("x", 4096)+`"}`)
		},
		"oversized refusal": func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"errors":"`+strings.Repeat("x", 4096)+`"}`)
		},
	} {
		provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			if request.Method == http.MethodGet {
				writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 3, nil))
				return
			}
			reply(response)
		})
		client, err := freshdesk.New(freshdesk.Config{Domain: testDomain, MaxResponseBytes: 2048}, testCredentialProvider(), freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
		require.NoError(t, err)
		_, err = sdkgo.RunMutation(newFreshdeskDexContext("update-retry"), client.UpdateTicket(), freshdeskConnection, freshdesk.UpdateTicketInput{
			TicketID: 20, Status: freshdesk.TicketStatusOpen,
		})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "%s: the write sets absolute values, so repeating it is safe", name)
		require.NotContains(t, err.Error(), testAPIKey, name)
		require.Equal(t, 2, provider.requestCount(), name)
	}
}

func TestUpdateTicketBranches(t *testing.T) {
	for _, test := range []struct {
		name        string
		readStatus  int
		readBody    string
		writeStatus int
		writeBody   string
		branch      sdkgo.BranchID
	}{
		{name: "missing ticket", readStatus: http.StatusNotFound, branch: freshdesk.UpdateTicketBranchNotFound},
		{name: "missing ticket on write", readStatus: http.StatusOK, writeStatus: http.StatusNotFound, branch: freshdesk.UpdateTicketBranchNotFound},
		{name: "unprocessable change", readStatus: http.StatusOK, writeStatus: http.StatusUnprocessableEntity, branch: freshdesk.UpdateTicketBranchProviderRejected},
		{name: "missing required field", readStatus: http.StatusOK, writeStatus: http.StatusBadRequest,
			writeBody: `{"errors":[{"field":"cf_reason","code":"missing_field"}]}`, branch: freshdesk.UpdateTicketBranchProviderRejected},
		{name: "invalid ticket read before the change", readStatus: http.StatusOK, readBody: `{"id":20}`, branch: freshdesk.UpdateTicketBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					if test.readStatus != http.StatusOK {
						writeJSON(t, response, test.readStatus, ``)
						return
					}
					if test.readBody != "" {
						writeJSON(t, response, http.StatusOK, test.readBody)
						return
					}
					writeJSON(t, response, http.StatusOK, ticketBodyJSON(20, 3, nil))
					return
				}
				writeJSON(t, response, test.writeStatus, test.writeBody)
			})
			result, err := sdkgo.RunMutation(newFreshdeskDexContext("update-"+test.name), newFreshdeskClient(t, provider.URL).UpdateTicket(), freshdeskConnection, freshdesk.UpdateTicketInput{
				TicketID: 20, Status: freshdesk.TicketStatusResolved,
			})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

func TestUpdateTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]freshdesk.UpdateTicketInput{
		"no ticket":       {Status: freshdesk.TicketStatusOpen},
		"no change":       {TicketID: 20},
		"status 1":        {TicketID: 20, Status: 1},
		"priority 0 only": {TicketID: 20, Priority: 0},
		"priority 9":      {TicketID: 20, Priority: 9},
		"add and remove":  {TicketID: 20, AddTags: []string{"vip"}, RemoveTags: []string{"VIP"}},
		"negative agent":  {TicketID: 20, ResponderID: -1},
	} {
		result, err := sdkgo.RunMutation(newFreshdeskDexContext("invalid-update"), newFreshdeskClient(t, provider.URL).UpdateTicket(), freshdeskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, freshdesk.UpdateTicketBranchDefect, result.Branch, name)
	}
}
