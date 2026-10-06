// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetTicketReturnsTheRequesterAndLatestMessagesNewestFirst(t *testing.T) {
	messages := []map[string]any{
		messageJSON(1, 5512, "email", false, "2026-01-26T14:02:00.000001", nil),
		messageJSON(3, 5512, "internal-note", true, "2026-01-27T10:05:00", "dex-call"),
		messageJSON(2, 5512, "email", true, "2026-01-27T10:00:00", nil),
	}
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", []string{"billing", "vip"}, messages))
	})
	result, err := sdkgo.RunQuery(newGorgiasDexContext("get"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection,
		gorgias.GetTicketInput{TicketID: 5512, LatestMessageLimit: 2})
	require.NoError(t, err)
	require.Equal(t, gorgias.GetTicketBranchFound, result.Branch)
	details := result.Value
	require.Equal(t, gorgias.TicketStatusOpen, details.Ticket.Status)
	require.Equal(t, []string{"billing", "vip"}, details.Ticket.Tags)
	require.Equal(t, "ERP-88213", details.Ticket.ExternalID)
	require.Equal(t, &gorgias.Customer{ID: 3924, Email: "jane@acme.example.com", Name: "Jane Smith", ExternalID: "cont_010"}, details.Requester)
	require.Len(t, details.LatestMessages, 2)
	require.Equal(t, int64(3), details.LatestMessages[0].ID)
	require.False(t, details.LatestMessages[0].IsPublic, "an internal note is never public")
	require.Equal(t, gorgias.MessageChannelInternalNote, details.LatestMessages[0].Channel)
	require.Equal(t, "dex-call", details.LatestMessages[0].ExternalID)
	require.Equal(t, int64(2), details.LatestMessages[1].ID)
	require.True(t, details.LatestMessages[1].IsPublic)
	require.True(t, details.LatestMessages[1].IsFromAgent)
	require.True(t, details.HasOlderMessages)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "HTML bodies, notes, and meta are not carried")
}

func TestGetTicketTruncatesLongBodiesAndRejectsForeignMessages(t *testing.T) {
	long := messageJSON(1, 5512, "email", false, "2026-01-26T14:02:00", nil)
	long["body_text"] = string(make([]byte, gorgias.MaxTextBytes+10))
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{long}))
			return
		}
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{messageJSON(9, 777, "email", false, "2026-01-26T14:02:00", nil)}))
	})
	client := newGorgiasClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newGorgiasDexContext("long"), client.GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Len(t, result.Value.LatestMessages[0].Body, gorgias.MaxTextBytes)
	require.True(t, result.Value.LatestMessages[0].IsBodyTruncated)
	result, err = sdkgo.RunQuery(newGorgiasDexContext("foreign"), client.GetTicket(), gorgiasConnection, gorgias.GetTicketInput{TicketID: 5512})
	require.NoError(t, err)
	require.Equal(t, gorgias.GetTicketBranchInvalidResponse, result.Branch)
}

func TestGetTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []gorgias.GetTicketInput{{}, {TicketID: 1, LatestMessageLimit: gorgias.MaxLatestMessageLimit + 1}, {TicketID: 1, LatestMessageLimit: -1}} {
		result, err := sdkgo.RunQuery(newGorgiasDexContext("invalid"), newGorgiasClient(t, provider.URL).GetTicket(), gorgiasConnection, input)
		require.NoError(t, err)
		require.Equal(t, gorgias.GetTicketBranchDefect, result.Branch)
	}
}

func TestFindCustomerByEmailMatchesThePrimaryAddressIgnoringCase(t *testing.T) {
	for name, body := range map[string]string{
		"list envelope": `{"object":"list","data":[{"id":3924,"email":"Jane@Acme.example.com","name":"Jane Smith","external_id":"cont_010","note":"SENTINEL"},
			{"id":8,"email":"jane@acme.example.com.au"}],"meta":{"next_cursor":"WyJuZXh0Il0="}}`,
		"bare array": `[{"id":3924,"email":"Jane@Acme.example.com","name":"Jane Smith","external_id":"cont_010"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newGorgiasDexContext("customer"), newGorgiasClient(t, provider.URL).FindCustomerByEmail(), gorgiasConnection,
				gorgias.FindCustomerByEmailInput{Email: "jane@acme.example.com"})
			require.NoError(t, err)
			require.Equal(t, gorgias.FindCustomerByEmailBranchFound, result.Branch)
			require.Equal(t, []gorgias.Customer{{ID: 3924, Email: "Jane@Acme.example.com", Name: "Jane Smith", ExternalID: "cont_010"}}, result.Value.Customers)
			require.Equal(t, name == "list envelope", result.Value.HasMore)
			require.Equal(t, "/api/customers", provider.request(0).path)
			require.Equal(t, "jane@acme.example.com", provider.request(0).query["email"][0])
			require.Equal(t, "3924", result.Receipt.ProviderObjectID)
		})
	}
}

func TestFindCustomerByEmailBranches(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, `{"data":[],"meta":{"next_cursor":null}}`)
		case 1:
			writeJSON(t, response, http.StatusForbidden, `{"error":{"msg":"SENTINEL"}}`)
		default:
			writeJSON(t, response, http.StatusOK, `{"data":[{"email":"x@example.com"}]}`)
		}
	})
	client := newGorgiasClient(t, provider.URL)
	for _, branch := range []sdkgo.BranchID{
		gorgias.FindCustomerByEmailBranchNotFound, gorgias.FindCustomerByEmailBranchProviderRejected, gorgias.FindCustomerByEmailBranchInvalidResponse,
	} {
		result, err := sdkgo.RunQuery(newGorgiasDexContext("branches"), client.FindCustomerByEmail(), gorgiasConnection, gorgias.FindCustomerByEmailInput{Email: "jane@example.com"})
		require.NoError(t, err)
		require.Equal(t, branch, result.Branch)
	}
	result, err := sdkgo.RunQuery(newGorgiasDexContext("invalid"), client.FindCustomerByEmail(), gorgiasConnection, gorgias.FindCustomerByEmailInput{Email: "not an email"})
	require.NoError(t, err)
	require.Equal(t, gorgias.FindCustomerByEmailBranchDefect, result.Branch)
	require.Equal(t, 3, provider.requestCount())
}
