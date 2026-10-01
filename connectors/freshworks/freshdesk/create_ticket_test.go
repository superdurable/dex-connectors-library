// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateTicketInput() freshdesk.CreateTicketInput {
	return freshdesk.CreateTicketInput{
		Subject: "Double charge on order 88213", Description: "I was charged <b>twice</b>.\nOrder 88213 & 88214.",
		Requester: freshdesk.TicketRequesterInput{Email: "jane@acme.example.com", Name: "Jane Smith"},
		Status:    freshdesk.TicketStatusOpen, Priority: freshdesk.TicketPriorityHigh, Type: "Problem",
		Tags: []string{"billing-double-charge"}, GroupID: 156, ResponderID: 6001263404,
	}
}

func TestCreateTicketSendsEscapedHTMLAndFreshdeskIntegersOnce(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1201, 2, []string{"billing-double-charge"}))
	})
	ctx := newFreshdeskDexContext("create")
	result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, int64(1201), result.Value.Ticket.ID)
	require.Equal(t, "1201", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v2/tickets", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Empty(t, request.header.Get("Idempotency-Key"), "Freshdesk documents no key, and Go would retry a keyed POST on its own")
	require.JSONEq(t, `{"subject":"Double charge on order 88213","description":"I was charged &lt;b&gt;twice&lt;/b&gt;.<br>Order 88213 &amp; 88214.",
		"email":"jane@acme.example.com","name":"Jane Smith","status":2,"priority":3,"type":"Problem","tags":["billing-double-charge"],
		"group_id":156,"responder_id":6001263404}`, request.body)
	require.JSONEq(t, `{"freshdeskDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
}

func TestCreateTicketOmitsUnsetFieldsForFreshdeskDefaults(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1202, 2, nil))
	})
	_, err := sdkgo.RunMutation(newFreshdeskDexContext("create-defaults"), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, freshdesk.CreateTicketInput{
		Subject: "Password reset", Description: "Please reset it.", Requester: freshdesk.TicketRequesterInput{Email: "ben@example.com"},
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"subject":"Password reset","description":"Please reset it.","email":"ben@example.com"}`, provider.request(0).body)
}

func TestCreateTicketNeverResendsARequestWhoseOutcomeIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error","message":"SENTINEL"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusGatewayTimeout, ``)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) },
			branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureTransport},
		{name: "malformed ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":"SENTINEL"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "reflected key", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":1,"subject":"`+testAPIKey+`"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "validation", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"description":"SENTINEL","errors":[{"field":"group_id","message":"SENTINEL","code":"invalid_value"}]}`)
		}, branch: freshdesk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "authentication", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnauthorized, `{"code":"invalid_credentials"}`)
		}, branch: freshdesk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureAuthentication},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newFreshdeskDexContext("create-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
			require.NoError(t, err, "only a provable non-application is retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
		})
	}
}

func TestCreateTicketRetriesOnlyWhatFreshdeskProvablyDidNotApply(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "34")
		writeJSON(t, response, http.StatusTooManyRequests, `{"code":"SENTINEL"}`)
	})
	ctx := newFreshdeskDexContext("create-rate-limited")
	_, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 34*time.Second, retryAfter.After)
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the marker so the retry may send")
	require.Equal(t, 2, ctx.heartbeatCount)

	refused := newFreshdeskDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newFreshdeskClient(t, closedLoopbackURL(t)).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Nil(t, refused.recordedHeartbeat, "a refused connection sent nothing")

	next := ctx.nextAttempt()
	created := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1203, 2, nil))
	})
	result, err := sdkgo.RunMutation(next, newFreshdeskClient(t, created.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
}

func TestCreateTicketAfterAnEarlierDispatchSendsNothing(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newFreshdeskDexContext("create-replay")
	first.recordedHeartbeat = json.RawMessage(`{"freshdeskDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", result.Failure.Message)

	unrecordable := newFreshdeskDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(unrecordable, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestCreateTicketRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, change := range map[string]func(*freshdesk.CreateTicketInput){
		"blank subject":     func(input *freshdesk.CreateTicketInput) { input.Subject = " " },
		"blank description": func(input *freshdesk.CreateTicketInput) { input.Description = "" },
		"display address":   func(input *freshdesk.CreateTicketInput) { input.Requester.Email = "Jane <jane@example.com>" },
		"status 1":          func(input *freshdesk.CreateTicketInput) { input.Status = 1 },
		"priority 5":        func(input *freshdesk.CreateTicketInput) { input.Priority = 5 },
		"padded type":       func(input *freshdesk.CreateTicketInput) { input.Type = " Problem" },
		"quoted tag":        func(input *freshdesk.CreateTicketInput) { input.Tags = []string{"it's"} },
		"negative group":    func(input *freshdesk.CreateTicketInput) { input.GroupID = -1 },
		"invalid UTF-8":     func(input *freshdesk.CreateTicketInput) { input.Description = strings.Repeat("\xff", 3) },
	} {
		input := validCreateTicketInput()
		change(&input)
		ctx := newFreshdeskDexContext("invalid-create")
		result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, freshdesk.CreateTicketBranchDefect, result.Branch, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}
