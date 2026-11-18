// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
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
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "missing", status: http.StatusNotFound, branch: gorgias.UpdateTicketBranchNotFound},
		{name: "rejected", status: http.StatusBadRequest, branch: gorgias.UpdateTicketBranchProviderRejected},
		{name: "outage", status: http.StatusInternalServerError, isRetry: true},
		{name: "conflict", status: http.StatusConflict, isRetry: true},
		{name: "redirect", status: http.StatusMovedPermanently, isRetry: true},
		{name: "invalid updated ticket", status: http.StatusOK, body: `{"id":"SENTINEL"}`, isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, nil))
					return
				}
				body := `{"error":{"msg":"SENTINEL"}}`
				if test.body != "" {
					body = test.body
				}
				writeJSON(t, response, test.status, body)
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

func TestUpdateTicketFirstReadSelectsABranchOnlyForADefiniteRefusal(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "missing", status: http.StatusNotFound, body: `{}`, branch: gorgias.UpdateTicketBranchNotFound},
		{name: "forbidden", status: http.StatusForbidden, body: `{}`, branch: gorgias.UpdateTicketBranchProviderRejected},
		{name: "invalid ticket", status: http.StatusOK, body: `{"id":"SENTINEL"}`, isRetry: true},
		{name: "oversized ticket", status: http.StatusOK, body: `{"id":5512,"subject":"` + strings.Repeat("x", 8192) + `"}`, isRetry: true},
		{name: "oversized refusal", status: http.StatusBadRequest, body: `{"error":{"msg":"` + strings.Repeat("x", 8192) + `"}}`, isRetry: true},
		{name: "redirect", status: http.StatusMovedPermanently, body: `{}`, isRetry: true},
		{name: "conflict", status: http.StatusConflict, body: `{}`, isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodGet, request.Method, "nothing is written after an unusable first read")
				writeJSON(t, response, test.status, test.body)
			})
			client, err := gorgias.New(gorgias.Config{Domain: testDomain, MaxResponseBytes: 4096}, testCredentialProvider(), gorgias.WithAPIBaseURL(provider.URL+"/api"))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(newGorgiasDexContext("update-read-"+test.name), client.UpdateTicket(), gorgiasConnection,
				gorgias.UpdateTicketInput{TicketID: 5512, Status: gorgias.TicketStatusClosed})
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "an earlier attempt's absolute change may have applied")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
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

func TestUpdateTicketRefusalWithAnOversizedBodyRetries(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "closed", nil, nil))
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"`+strings.Repeat("x", 8192)+`"}}`)
	})
	client, err := gorgias.New(gorgias.Config{Domain: testDomain, MaxResponseBytes: 4096}, testCredentialProvider(), gorgias.WithAPIBaseURL(provider.URL+"/api"))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(newGorgiasDexContext("update-oversized"), client.UpdateTicket(), gorgiasConnection,
		gorgias.UpdateTicketInput{TicketID: 5512, Status: gorgias.TicketStatusOpen})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an unread refusal body cannot prove the absolute change was refused")
}

func TestUpdateTicketCredentialFailureRetriesOnlyWhenTransient(t *testing.T) {
	for _, test := range credentialFailureCases {
		provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
		result, err := sdkgo.RunMutation(newGorgiasDexContext("update-credentials"), newGorgiasClientWithCredentials(t, provider.URL, test.credentials).UpdateTicket(),
			gorgiasConnection, gorgias.UpdateTicketInput{TicketID: 5512, Status: gorgias.TicketStatusOpen})
		if !test.isTerminal {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, test.name)
			continue
		}
		require.NoError(t, err, test.name)
		require.Equal(t, gorgias.UpdateTicketBranchDefect, result.Branch, test.name)
	}
}

func TestUpdateTicketTerminalCredentialFailureSelectsDefectOnlyOnTheFirstAttempt(t *testing.T) {
	for _, test := range credentialFailureCases {
		provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
		later := newGorgiasDexContext("update-credentials-later").nextAttempt()
		_, err := sdkgo.RunMutation(later, newGorgiasClientWithCredentials(t, provider.URL, test.credentials).UpdateTicket(),
			gorgiasConnection, gorgias.UpdateTicketInput{TicketID: 5512, Status: gorgias.TicketStatusOpen})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "%s: an earlier attempt's change may have applied", test.name)
	}
}
