// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddNoteAddsAnInternalNoteAsTheConnectionUser(t *testing.T) {
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		note := messageJSON(77, 5512, "internal-note", true, "2026-01-28T09:00:00", externalIDOf(t, provider.request(index).body))
		note["body_text"], note["sent_datetime"] = "Refund issued <REF-771>.", nil
		writeValue(t, response, http.StatusCreated, note)
	})
	ctx := newGorgiasDexContext("note")
	result, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection, gorgias.AddNoteInput{TicketID: 5512, Body: "Refund issued <REF-771>."})
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
	request := provider.request(0)
	require.Equal(t, "/api/tickets/5512/messages", request.path)
	key := string(result.Receipt.IdempotencyKey)
	require.True(t, strings.HasPrefix(key, "dex-"))
	require.JSONEq(t, `{"channel":"internal-note","via":"api","from_agent":true,"public":false,"sender":{"email":"`+testEmail+`"},
		"body_text":"Refund issued <REF-771>.","body_html":"Refund issued &lt;REF-771&gt;.","external_id":"`+key+`"}`, request.body)
	message := result.Value.Message
	require.Equal(t, int64(77), message.ID)
	require.False(t, message.IsPublic)
	require.Equal(t, gorgias.MessageChannelInternalNote, message.Channel)
	require.Nil(t, message.SentAt)
	require.Equal(t, "77", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, ctx.recordedHeartbeat)
}

func TestAddNoteRoutesAPublicReplyToTheCustomersNewestEmail(t *testing.T) {
	older := messageJSON(1, 5512, "email", false, "2026-01-26T14:02:00", nil)
	newer := messageJSON(2, 5512, "email", false, "2026-01-27T10:00:00", nil)
	newer["source"] = map[string]any{"type": "email", "from": map[string]any{"address": "jane.smith@acme.example.com"},
		"to": []map[string]any{{"address": "billing@shop.example.com"}}}
	newer["subject"] = "Still double charged"
	agentReply := messageJSON(3, 5512, "email", true, "2026-01-28T10:00:00", nil)
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{older, agentReply, newer}))
			return
		}
		reply := messageJSON(78, 5512, "email", true, "2026-01-28T11:00:00", externalIDOf(t, provider.request(index).body))
		reply["sent_datetime"] = nil
		writeValue(t, response, http.StatusCreated, reply)
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("reply"), newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection,
		gorgias.AddNoteInput{TicketID: 5512, Body: "We refunded the duplicate charge.", IsPublicReply: true})
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.JSONEq(t, `{"channel":"email","via":"api","from_agent":true,"public":true,"sender":{"email":"`+testEmail+`"},
		"receiver":{"email":"jane.smith@acme.example.com"},"source":{"type":"email","from":{"address":"billing@shop.example.com"},
		"to":[{"address":"jane.smith@acme.example.com"}]},"subject":"Re: Still double charged","body_text":"We refunded the duplicate charge.",
		"body_html":"We refunded the duplicate charge.","external_id":"`+string(result.Receipt.IdempotencyKey)+`"}`, provider.request(1).body)
	require.True(t, result.Value.Message.IsPublic)
	require.Nil(t, result.Value.Message.SentAt, "Gorgias sends a reply asynchronously")
}

func TestAddNoteRepliesFromTheAddressAnAgentEmailThroughTheSameIntegrationUsed(t *testing.T) {
	customerEmail := messageJSON(1, 5512, "email", false, "2026-01-27T10:00:00", nil)
	customerEmail["integration_id"] = 7
	customerEmail["source"] = map[string]any{"type": "email", "from": map[string]any{"address": "jane@acme.example.com"},
		"to": []map[string]any{{"address": "alice@partner.example.com"}, {"address": "billing@shop.example.com"}}}
	otherIntegrationEmail := messageJSON(2, 5512, "email", true, "2026-01-28T09:00:00", nil)
	otherIntegrationEmail["integration_id"] = 8
	otherIntegrationEmail["source"] = map[string]any{"type": "email", "from": map[string]any{"address": "sales@shop.example.com"}}
	agentEmail := messageJSON(3, 5512, "email", true, "2026-01-26T09:00:00", nil)
	agentEmail["integration_id"] = 7
	agentEmail["source"] = map[string]any{"type": "email", "from": map[string]any{"address": "billing@shop.example.com"}}
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{customerEmail, otherIntegrationEmail, agentEmail}))
			return
		}
		writeValue(t, response, http.StatusCreated, messageJSON(78, 5512, "email", true, "2026-01-28T11:00:00", externalIDOf(t, provider.request(index).body)))
	})
	result, err := sdkgo.RunMutation(newGorgiasDexContext("integration-reply"), newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection,
		gorgias.AddNoteInput{TicketID: 5512, Body: "Refunded.", IsPublicReply: true})
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
	var sent struct {
		IntegrationID int64 `json:"integration_id"`
		Source        struct {
			From struct {
				Address string `json:"address"`
			} `json:"from"`
		} `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &sent))
	require.Equal(t, "billing@shop.example.com", sent.Source.From.Address, "not the first To address, which no integration owns")
	require.Equal(t, int64(7), sent.IntegrationID)
}

func TestAddNoteRoutesAReplyOnlyFromAKnownSupportAddress(t *testing.T) {
	for _, test := range []struct {
		name        string
		to          []string
		cc          []string
		fromAddress string
	}{
		{name: "partner first and support in CC", to: []string{"alice@partner.example.com"}, cc: []string{"billing@shop.example.com"}},
		{name: "two To addresses", to: []string{"alice@partner.example.com", "billing@shop.example.com"}},
		{name: "customer copied themselves", to: []string{"billing@shop.example.com"}, cc: []string{"JANE@acme.example.com"}, fromAddress: "billing@shop.example.com"},
		{name: "support only in CC", cc: []string{"billing@shop.example.com"}, fromAddress: "billing@shop.example.com"},
	} {
		t.Run(test.name, func(t *testing.T) {
			customerEmail := messageJSON(1, 5512, "email", false, "2026-01-27T10:00:00", nil)
			customerEmail["source"] = map[string]any{"type": "email", "from": map[string]any{"address": "jane@acme.example.com"},
				"to": addressesJSON(test.to), "cc": addressesJSON(test.cc)}
			var provider *recordingGorgias
			provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
				if request.Method == http.MethodGet {
					writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{customerEmail}))
					return
				}
				writeValue(t, response, http.StatusCreated, messageJSON(78, 5512, "email", true, "2026-01-28T11:00:00", externalIDOf(t, provider.request(index).body)))
			})
			ctx := newGorgiasDexContext("support-address")
			result, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection,
				gorgias.AddNoteInput{TicketID: 5512, Body: "Refunded.", IsPublicReply: true})
			require.NoError(t, err)
			if test.fromAddress == "" {
				require.Equal(t, gorgias.AddNoteBranchProviderRejected, result.Branch)
				require.Contains(t, result.Failure.Message, "none is known to be the helpdesk's")
				require.Equal(t, 1, provider.requestCount(), "a reply from an address no integration owns is never sent")
				require.Nil(t, ctx.recordedHeartbeat)
				return
			}
			require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
			require.Contains(t, provider.request(1).body, `"from":{"address":"`+test.fromAddress+`"}`)
			require.NotContains(t, provider.request(1).body, "integration_id", "a message without an integration sends none")
		})
	}
}

func addressesJSON(addresses []string) []map[string]any {
	var values []map[string]any
	for _, address := range addresses {
		values = append(values, map[string]any{"address": address})
	}
	return values
}

func TestAddNoteRejectsAReplyToATicketWithoutACustomerEmail(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{
			messageJSON(1, 5512, "api", false, "2026-01-26T14:02:00", nil), messageJSON(2, 5512, "email", true, "2026-01-27T10:00:00", nil),
		}))
	})
	ctx := newGorgiasDexContext("no-email")
	result, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection,
		gorgias.AddNoteInput{TicketID: 5512, Body: "Hello", IsPublicReply: true})
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "no email from the customer")
	require.Equal(t, 1, provider.requestCount(), "nothing is sent")
	require.Nil(t, ctx.recordedHeartbeat, "no checkpoint is recorded before a request is due")
}

func TestAddNoteWithALostResponseFindsTheMessageByExternalIDWithoutResending(t *testing.T) {
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if request.Method == http.MethodPost {
			dropConnection(t, response)
			return
		}
		sentKey := externalIDOf(t, provider.request(0).body)
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{
			messageJSON(1, 5512, "internal-note", true, "2026-01-26T14:02:00", "dex-another-step"),
			messageJSON(2, 5512, "internal-note", true, "2026-01-28T09:00:00", sentKey),
		}))
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("lost-note")
	_, err := sdkgo.RunMutation(first, client.AddNote(), gorgiasConnection, gorgias.AddNoteInput{TicketID: 5512, Body: "Triaged."})
	require.Error(t, err, "an unconfirmed note is retried only to look it up")
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AddNote(), gorgiasConnection, gorgias.AddNoteInput{TicketID: 5512, Body: "Triaged."})
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, int64(2), result.Value.Message.ID)
	require.Equal(t, 2, provider.requestCount())
}

func TestAddNoteWithAnEarlierDispatchAndUnavailableCredentialsNeverSelectsDefect(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newGorgiasClientWithCredentials(t, provider.URL, sdkgo.StaticCredentialProvider[gorgias.Credentials]{})
	input := gorgias.AddNoteInput{TicketID: 5512, Body: "Triaged.", IsPublicReply: true}
	lookingAgain := newMarkedDexContext("note-credentials", 2)
	_, err := sdkgo.RunMutation(lookingAgain, client.AddNote(), gorgiasConnection, input)
	require.Error(t, err, "the marker is kept and the lookup waits for usable credentials")
	require.NotEmpty(t, lookingAgain.recordedHeartbeat)

	result, err := sdkgo.RunMutation(newMarkedDexContext("note-credentials", 4), client.AddNote(), gorgiasConnection, input)
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchUncertain, result.Branch, "a possibly sent reply is never reported as a defect")
}

func TestAddNoteLaterAttemptWithoutAMarkerFindsTheMessageInsteadOfResending(t *testing.T) {
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			dropConnection(t, response)
			return
		}
		writeValue(t, response, http.StatusOK, ticketJSON(5512, "open", nil, []map[string]any{
			messageJSON(2, 5512, "internal-note", true, "2026-01-28T09:00:00", externalIDOf(t, provider.request(0).body)),
		}))
	})
	client := newGorgiasClient(t, provider.URL)
	input := gorgias.AddNoteInput{TicketID: 5512, Body: "Triaged."}
	first := newGorgiasDexContext("unstored-marker")
	_, err := sdkgo.RunMutation(first, client.AddNote(), gorgiasConnection, input)
	require.Error(t, err)
	// Dex lost the first attempt's marker before storing it, so the next attempt sees none.
	unmarked := &gorgiasDexContext{Context: first.Context, step: first.step, attempt: 2}
	result, err := sdkgo.RunMutation(unmarked, client.AddNote(), gorgiasConnection, input)
	require.NoError(t, err)
	require.Equal(t, gorgias.AddNoteBranchAdded, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, int64(2), result.Value.Message.ID)
	require.Equal(t, 2, provider.requestCount(), "the note is looked up, not sent again")
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestAddNoteBranches(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing ticket", status: http.StatusNotFound, branch: gorgias.AddNoteBranchNotFound},
		{name: "rejected", status: http.StatusBadRequest, body: `{"error":{"msg":"SENTINEL","data":{"sender":["x"]}}}`, branch: gorgias.AddNoteBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newGorgiasDexContext("note-"+test.name), newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection,
				gorgias.AddNoteInput{TicketID: 3, Body: "Hello"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for _, input := range []gorgias.AddNoteInput{{Body: "Hello"}, {TicketID: 3, Body: "  "}} {
		result, err := sdkgo.RunMutation(newGorgiasDexContext("invalid-note"), newGorgiasClient(t, provider.URL).AddNote(), gorgiasConnection, input)
		require.NoError(t, err)
		require.Equal(t, gorgias.AddNoteBranchDefect, result.Branch)
	}
}
