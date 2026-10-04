// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateTicketInput() gorgias.CreateTicketInput {
	return gorgias.CreateTicketInput{
		Subject: "Double charge on order 88213", Description: "I was charged twice <order 88213>.\nPlease help.",
		Requester: gorgias.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    gorgias.TicketStatusOpen, Priority: gorgias.TicketPriorityHigh, Tags: []string{"billing"}, AssigneeTeamID: 8,
	}
}

func externalIDOf(t *testing.T, body string) string {
	t.Helper()
	var payload struct {
		ExternalID string `json:"external_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &payload))
	return payload.ExternalID
}

func TestCreateTicketSendsTheFirstMessageThroughTheAPIChannelWithTheIdempotencyKey(t *testing.T) {
	var provider *recordingGorgias
	provider = newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		ticket := ticketJSON(5512, "open", []string{"billing"}, nil)
		ticket["external_id"] = externalIDOf(t, provider.request(index).body)
		writeValue(t, response, http.StatusCreated, ticket)
	})
	ctx := newGorgiasDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.False(t, result.Value.WasCreatedByEarlierAttempt)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/tickets", request.path)
	key := string(result.Receipt.IdempotencyKey)
	require.True(t, strings.HasPrefix(key, "dex-"))
	require.JSONEq(t, `{"customer":{"email":"jane@acme.example.com","name":"Jane Smith"},"subject":"Double charge on order 88213",
		"channel":"api","via":"api","from_agent":false,"status":"open","priority":"high","tags":[{"name":"billing"}],"assignee_team":{"id":8},
		"external_id":"`+key+`","messages":[{"channel":"api","via":"api","from_agent":false,"sender":{"email":"jane@acme.example.com","name":"Jane Smith"},
		"subject":"Double charge on order 88213","body_text":"I was charged twice <order 88213>.\nPlease help.",
		"body_html":"I was charged twice &lt;order 88213&gt;.<br>Please help.","stripped_text":"I was charged twice <order 88213>.\nPlease help."}]}`, request.body)
	require.Equal(t, key, result.Value.Ticket.ExternalID)
	require.Equal(t, "5512", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, ctx.recordedHeartbeat, "the dispatch checkpoint is recorded before sending")
}

func TestCreateTicketWithALostResponseFindsTheTicketByExternalIDWithoutResending(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		ticket := ticketJSON(5512, "open", nil, nil)
		delete(ticket, "messages")
		ticket["external_id"] = request.URL.Query().Get("external_id")
		other := ticketJSON(9, "open", nil, nil)
		delete(other, "messages")
		writeValue(t, response, http.StatusOK, ticketPage(nil, other, ticket))
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("lost-create")
	_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter, "an unconfirmed create is retried only to look the ticket up")
	require.Equal(t, 2*time.Second, retryAfter.After)

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, int64(5512), result.Value.Ticket.ID)
	require.Equal(t, 2, provider.requestCount())
	lookup := provider.request(1)
	require.Equal(t, http.MethodGet, lookup.method)
	require.Equal(t, externalIDOf(t, provider.request(0).body), lookup.query["external_id"][0])
}

func TestCreateTicketReconcileOutcomes(t *testing.T) {
	for _, test := range []struct {
		name         string
		status       int
		body         string
		retryAfter   string
		retryDelay   time.Duration
		isConclusive bool
		contains     string
	}{
		{name: "no ticket", status: http.StatusOK, body: `{"data":[],"meta":{"next_cursor":null}}`, retryDelay: 15 * time.Second, contains: "shows no ticket"},
		{name: "lookup outage", status: http.StatusBadGateway, body: `{"error":{"msg":"SENTINEL"}}`, retryAfter: "30", retryDelay: 30 * time.Second, contains: "could not confirm"},
		{name: "invalid lookup", status: http.StatusOK, body: `{"meta":{}}`, retryDelay: 15 * time.Second, contains: "could not confirm"},
		{name: "rate limited lookup", status: http.StatusTooManyRequests, body: `{"error":{"msg":"SENTINEL"}}`, retryAfter: "600", retryDelay: time.Minute, contains: "could not confirm"},
		{name: "rejected lookup", status: http.StatusForbidden, body: `{"error":{"msg":"SENTINEL"}}`, isConclusive: true, contains: "could not confirm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			client := newGorgiasClient(t, provider.URL)
			if !test.isConclusive {
				lookingAgain := newMarkedDexContext("reconcile", 2)
				_, err := sdkgo.RunMutation(lookingAgain, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter, "a read-only lookup is repeated while the lookup budget lasts")
				require.Equal(t, test.retryDelay, retryAfter.After)
				require.NotEmpty(t, lookingAgain.recordedHeartbeat, "the marker is kept for the next lookup")
			}
			result, err := sdkgo.RunMutation(newMarkedDexContext("reconcile", 4), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
			require.NoError(t, err)
			require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch)
			require.Contains(t, result.Failure.Message, test.contains)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			for index := range provider.requestCount() {
				require.Equal(t, http.MethodGet, provider.request(index).method, "a marked attempt never resends")
			}
		})
	}
}

func TestCreateTicketWithAnEarlierDispatchAndUnavailableCredentialsNeverSelectsDefect(t *testing.T) {
	provider := newRecordingGorgias(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	client := newGorgiasClientWithCredentials(t, provider.URL, sdkgo.StaticCredentialProvider[gorgias.Credentials]{})
	lookingAgain := newMarkedDexContext("credentials", 2)
	_, err := sdkgo.RunMutation(lookingAgain, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter, "the marker is kept and the lookup waits for usable credentials")
	require.NotEmpty(t, lookingAgain.recordedHeartbeat)

	result, err := sdkgo.RunMutation(newMarkedDexContext("credentials", 4), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchUncertain, result.Branch, "a possibly sent ticket is never reported as a defect")
	require.Contains(t, result.Failure.Message, "credentials are unavailable")
}

func TestCreateTicketWaitsForRetryAfterBeforeLookingForAnUnconfirmedTicket(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "20")
		writeJSON(t, response, http.StatusServiceUnavailable, `{"error":{"msg":"SENTINEL"}}`)
	})
	ctx := newGorgiasDexContext("unavailable")
	_, err := sdkgo.RunMutation(ctx, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 20*time.Second, retryAfter.After)
	require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so the retry only looks the ticket up")
}

func TestCreateTicketRetriesOnlyWhatGorgiasProvablyDidNotApply(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case index == 0:
			response.Header().Set("Retry-After", "3")
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":{"msg":"SENTINEL"}}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, `{"data":[],"meta":{"next_cursor":null}}`)
		default:
			writeValue(t, response, http.StatusCreated, ticketJSON(5512, "open", nil, nil))
		}
	})
	client := newGorgiasClient(t, provider.URL)
	first := newGorgiasDexContext("rate-limited")
	_, err := sdkgo.RunMutation(first, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
	require.Nil(t, first.recordedHeartbeat, "a 429 clears the checkpoint, so the retry may send")
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 3, provider.requestCount())
	lookup := provider.request(1)
	require.Equal(t, http.MethodGet, lookup.method, "a later attempt looks for the ticket before it sends")
	require.Equal(t, externalIDOf(t, provider.request(0).body), lookup.query["external_id"][0])
	require.Equal(t, http.MethodPost, provider.request(2).method)
}

func TestCreateTicketLaterAttemptWithoutAMarkerFindsTheTicketInsteadOfResending(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		ticket := ticketJSON(5512, "open", nil, nil)
		delete(ticket, "messages")
		ticket["external_id"] = request.URL.Query().Get("external_id")
		writeValue(t, response, http.StatusOK, ticketPage(nil, ticket))
	})
	unmarked := newGorgiasDexContext("unstored-marker").nextAttempt()
	result, err := sdkgo.RunMutation(unmarked, newGorgiasClient(t, provider.URL).CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchCreated, result.Branch)
	require.True(t, result.Value.WasCreatedByEarlierAttempt)
	require.Equal(t, 1, provider.requestCount(), "the ticket an earlier attempt sent before Dex stored its marker is not created again")
	require.Equal(t, http.MethodGet, provider.request(0).method)
}

func TestCreateTicketRejectionsAndDefects(t *testing.T) {
	provider := newRecordingGorgias(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"msg":"SENTINEL","data":{"assignee_team":["SENTINEL unknown"]}}}`)
	})
	client := newGorgiasClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newGorgiasDexContext("rejected"), client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, gorgias.CreateTicketBranchProviderRejected, result.Branch)
	require.Equal(t, "Gorgias rejected the request (HTTP 400) [fields: assignee_team]", result.Failure.Message)

	rejecting := newGorgiasDexContext("heartbeat")
	rejecting.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(rejecting, client.CreateTicket(), gorgiasConnection, validCreateTicketInput())
	require.Error(t, err, "nothing is sent until Dex records the checkpoint")
	require.Equal(t, 1, provider.requestCount())

	for name, change := range map[string]func(*gorgias.CreateTicketInput){
		"blank subject": func(input *gorgias.CreateTicketInput) { input.Subject = " " },
		"long subject": func(input *gorgias.CreateTicketInput) {
			input.Subject = strings.Repeat("s", gorgias.MaxSubjectLength+1)
		},
		"display requester": func(input *gorgias.CreateTicketInput) { input.Requester.Email = "Jane <jane@example.com>" },
		"zendesk status":    func(input *gorgias.CreateTicketInput) { input.Status = "pending" },
		"zendesk priority":  func(input *gorgias.CreateTicketInput) { input.Priority = "urgent" },
		"duplicate tag":     func(input *gorgias.CreateTicketInput) { input.Tags = []string{"a", "a"} },
	} {
		input := validCreateTicketInput()
		change(&input)
		result, err := sdkgo.RunMutation(newGorgiasDexContext("invalid"), client.CreateTicket(), gorgiasConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, gorgias.CreateTicketBranchDefect, result.Branch, name)
	}
	require.Equal(t, 1, provider.requestCount())
}
