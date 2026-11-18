// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"claimed"`, "the fence claim is recorded before the request")
	require.Contains(t, string(ctx.recordedHeartbeat), `"providerId":"1201"`)
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

func TestCreateTicketFirstSendOutcomes(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		// branch is empty for a Retry that keeps the claim, so the next attempt resends below the cap.
		branch    sdkgo.BranchID
		kind      sdkgo.FailureKind
		heartbeat string
		// readBacks counts ticket reads after an accepted ticket ID; each gets the same reply.
		readBacks int
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error","message":"SENTINEL"}`)
		}, kind: sdkgo.FailureAvailability, heartbeat: `"state":"claimed"`},
		{name: "gateway timeout", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusGatewayTimeout, ``)
		}, kind: sdkgo.FailureAvailability, heartbeat: `"state":"claimed"`},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) },
			kind: sdkgo.FailureTransport, heartbeat: `"state":"claimed"`},
		{name: "conflict", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusConflict, `{"message":"SENTINEL"}`)
		}, kind: sdkgo.FailureConflict, heartbeat: `"state":"claimed"`},
		{name: "unlisted client error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusGone, `{"message":"SENTINEL"}`)
		}, kind: sdkgo.FailureProviderRejection, heartbeat: `"state":"claimed"`},
		{name: "oversized refusal", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"errors":"`+strings.Repeat("x", 4096)+`"}`)
		}, kind: sdkgo.FailureValidation, heartbeat: `"state":"claimed"`},
		{name: "malformed ticket without an ID", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":"SENTINEL"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol, heartbeat: `"state":"claimed"`},
		{name: "oversized accepted ticket", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":1209,"description_text":"`+strings.Repeat("x", 4096)+`"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureResponseTooLarge, heartbeat: `"state":"claimed"`},
		{name: "reflected key", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":1,"subject":"`+testAPIKey+`"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol, heartbeat: `"state":"claimed"`},
		{name: "accepted ticket ID with an invalid field", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":1205,"subject":"SENTINEL","created_at":"SENTINEL"}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureProtocol, heartbeat: `"providerId":"1205"`, readBacks: 1},
		{name: "refusal carrying a created ID", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"id":1208}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, kind: sdkgo.FailureValidation, heartbeat: `"providerId":"1208"`, readBacks: 1},
		{name: "validation", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"description":"SENTINEL","errors":[{"field":"group_id","message":"SENTINEL","code":"invalid_value"}]}`)
		}, branch: freshdesk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureValidation, heartbeat: `"state":"canResend"`},
		{name: "authentication", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnauthorized, `{"code":"invalid_credentials"}`)
		}, branch: freshdesk.CreateTicketBranchProviderRejected, kind: sdkgo.FailureAuthentication, heartbeat: `"state":"canResend"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newFreshdeskDexContext("create-" + test.name)
			client, err := freshdesk.New(freshdesk.Config{Domain: testDomain, MaxResponseBytes: 2048}, testCredentialProvider(), freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
			require.Equal(t, 1+test.readBacks, provider.requestCount())
			require.Contains(t, string(ctx.recordedHeartbeat), test.heartbeat)
			if test.branch == "" {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "an unknown outcome below the send cap is resent by the next attempt")
				require.Equal(t, test.kind, retry.Failure.Kind)
				require.NotContains(t, err.Error(), "SENTINEL")
				require.NotContains(t, err.Error(), testAPIKey)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
		})
	}
}

func TestCreateTicketResendsUnknownOutcomesBelowTheCapAndSelectsUncertainAtTheCap(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadGateway, `{"code":"bad_gateway"}`)
	})
	client := newFreshdeskClient(t, provider.URL)
	attempt := newFreshdeskDexContext("create-cap")
	for sendCount := 1; sendCount < 3; sendCount++ {
		_, err := sdkgo.RunMutation(attempt, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "send %d leaves room under the cap of 3", sendCount)
		attempt = attempt.nextAttempt()
	}
	result, err := sdkgo.RunMutation(attempt, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "the third send reaches the cap")
	require.Equal(t, 3, provider.requestCount())

	result, err = sdkgo.RunMutation(attempt.nextAttempt(), client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, "earlier attempts may have sent the request and the send cap is reached, so it is not sent again", result.Failure.Message)
	require.Equal(t, 3, provider.requestCount(), "the fence refuses a fourth send")
}

func TestCreateTicketWithASendCapOfOneSelectsUncertainForAnUnknownOutcome(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	ctx := newFreshdeskDexContext("create-cap-one")
	result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput(), sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketResendsAfterAnUnknownOutcomeAndCreatesTheTicket(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1206, 2, nil))
	})
	first := newFreshdeskDexContext("create-resend")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, provider.request(0).body, provider.request(1).body, "the resend repeats the same ticket")
}

func TestCreateTicketRefusalAfterAnUnknownOutcomeSelectsUncertain(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			dropConnection(t, response)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"field":"group_id","code":"invalid_value"}]}`)
	})
	first := newFreshdeskDexContext("create-refusal-after-unknown")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.Error(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "the first send may have created the ticket")
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
}

// After an unknown send and a 429 resend, Decide allows a send with one prior send, so the
// connector still treats the call as possibly sent.
func TestCreateTicketCountsAnUnknownSendAfterARateLimitedResend(t *testing.T) {
	for _, test := range []struct {
		name        string
		thirdReply  func(http.ResponseWriter)
		credentials error
		branch      sdkgo.BranchID
		requests    int
	}{
		{name: "refusal", thirdReply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, `{"errors":[{"field":"group_id","code":"invalid_value"}]}`)
		}, branch: freshdesk.CreateTicketBranchUncertain, requests: 3},
		{name: "unknown outcome below the cap", thirdReply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadGateway, `{"code":"bad_gateway"}`)
		}, requests: 3},
		{name: "terminal credentials", credentials: projectconfig.ErrObjectNotFound, branch: freshdesk.CreateTicketBranchUncertain, requests: 2},
		{name: "transient credentials", credentials: errors.New("temporary credential store failure"), requests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				switch index {
				case 0:
					writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error"}`)
				case 1:
					writeJSON(t, response, http.StatusTooManyRequests, `{"code":"rate_limited"}`)
				default:
					test.thirdReply(response)
				}
			})
			client := newFreshdeskClient(t, provider.URL)
			first := newFreshdeskDexContext("create-after-429-" + test.name)
			_, err := sdkgo.RunMutation(first, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			second := first.nextAttempt()
			_, err = sdkgo.RunMutation(second, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
			require.ErrorAs(t, err, &retry)
			require.Contains(t, string(second.recordedHeartbeat), `"state":"canResend"`)
			require.Contains(t, string(second.recordedHeartbeat), `"sendCount":1`, "the 429 does not erase the unknown first send")
			third := second.nextAttempt()
			if test.credentials != nil {
				client, err = freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: test.credentials},
					freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
				require.NoError(t, err)
			}
			result, err := sdkgo.RunMutation(third, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
			require.Equal(t, test.requests, provider.requestCount())
			if test.credentials != nil {
				require.Nil(t, third.recordedHeartbeat, "notSent would hide the unknown first send")
			}
			if test.branch == "" {
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch, "neither providerRejected nor defect may follow a possible send")
		})
	}
}

func TestCreateTicketWithAnAcceptedIDIsNeverResent(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case request.Method == http.MethodPost:
			writeJSON(t, response, http.StatusCreated, `{"id":1207,"status":"open"}`)
		case index == 1:
			writeJSON(t, response, http.StatusServiceUnavailable, `{"code":"service_unavailable"}`)
		default:
			writeJSON(t, response, http.StatusOK, ticketBodyJSON(1207, 2, nil))
		}
	})
	first := newFreshdeskDexContext("create-accepted-id")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a transient read-back failure retries because the ticket ID is saved")
	require.Contains(t, string(first.recordedHeartbeat), `"providerId":"1207"`)
	require.Equal(t, "/api/v2/tickets/1207", provider.request(1).path)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch, "the saved ticket ID is read back, never resent")
	require.Equal(t, int64(1207), result.Value.Ticket.ID)
	require.Equal(t, "1207", result.Receipt.ProviderObjectID)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method)
}

func TestCreateTicketReadsAnAcceptedTicketBackInTheSameAttempt(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, `{"id":1210,"created_at":"SENTINEL"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, ticketBodyJSON(1210, 2, nil))
	})
	result, err := sdkgo.RunMutation(newFreshdeskDexContext("create-read-back"), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, int64(1210), result.Value.Ticket.ID)
	require.Equal(t, 2, provider.requestCount())
}

func TestCreateTicketAcceptedIDWithoutACheckpointNeverRetriesTheReadBack(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, `{"id":1211}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{"code":"internal_error"}`)
	})
	ctx := &providerIDRejectingContext{freshdeskDexContext: newFreshdeskDexContext("create-read-back-unrecorded")}
	result, err := sdkgo.RunMutation(ctx, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "without a saved ID a Retry could resend the ticket")
	require.Equal(t, 2, provider.requestCount())
}

func TestCreateTicketSavedTicketIDThatFreshdeskNoLongerShowsSelectsUncertain(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusCreated, `{"id":1212}`)
			return
		}
		writeJSON(t, response, http.StatusNotFound, `{}`)
	})
	result, err := sdkgo.RunMutation(newFreshdeskDexContext("create-read-back-missing"), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "a read-back 404 never selects a refusal and never resends")
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	require.Equal(t, 2, provider.requestCount())
}

// providerIDRejectingContext fails every heartbeat that carries a provider ID, as a lost checkpoint would.
type providerIDRejectingContext struct {
	*freshdeskDexContext
}

func (context *providerIDRejectingContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err == nil && strings.Contains(string(encoded), `"providerId"`) {
		return errors.New("heartbeat stream closed")
	}
	return context.freshdeskDexContext.RecordHeartbeat(value)
}

func TestCreateTicketRetriesRateLimitAndRefusedConnectionWithCanResend(t *testing.T) {
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"canResend"`, "a 429 was not processed")

	refused := newFreshdeskDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newFreshdeskClient(t, closedLoopbackURL(t)).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Contains(t, string(refused.recordedHeartbeat), `"state":"canResend"`, "a refused connection sent nothing")

	next := ctx.nextAttempt()
	created := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1203, 2, nil))
	})
	result, err := sdkgo.RunMutation(next, newFreshdeskClient(t, created.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
}

func TestCreateTicketRetriesWithoutSendingAfterAnUntrustedCheckpoint(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, marker := range map[string]string{
		"v0.24.0 dispatch marker": `{"freshdeskDispatchedCallId":"earlier","freshdeskDispatchedAttempt":1}`,
		"v0.24.0 retry permit":    `{"freshdeskDispatchedCallId":"earlier","freshdeskRetryAttempt":2}`,
		"unreadable":              `[]`,
	} {
		first := newFreshdeskDexContext("create-untrusted")
		first.recordedHeartbeat, first.heartbeatCount = json.RawMessage(marker), 1
		_, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "%s: FailClosed sends nothing and retries", name)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind, name)
		require.Equal(t, "the single-dispatch checkpoint could not be read; nothing was sent in this attempt", retry.Failure.Message, name)
	}

	unrecordable := newFreshdeskDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(unrecordable, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestCreateTicketCountsAMissingCheckpointAsAPossibleSend(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1208, 2, nil))
	})
	missing := newFreshdeskDexContext("create-missing-marker").nextAttempt()
	result, err := sdkgo.RunMutation(missing, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch, "one possible earlier send leaves room under the cap")
	require.Contains(t, string(missing.recordedHeartbeat), `"sendCount":2`)

	atCap := newFreshdeskDexContext("create-missing-marker-at-cap").nextAttempt().nextAttempt().nextAttempt()
	result, err = sdkgo.RunMutation(atCap, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "three earlier attempts may each have sent once")
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketReadsTheDispatchCheckpointBeforeResolvingCredentials(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1201, 2, nil))
	})
	first := newFreshdeskDexContext("create-credential-rotation")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: projectconfig.ErrObjectNotFound},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "the saved ticket ID is never resent")
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketCredentialFailureAfterAnUnprocessedSend(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"code":"service_unavailable"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1204, 2, nil))
	})
	first := newFreshdeskDexContext("create-credential-retry")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	transientClient, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: errors.New("temporary credential store failure")},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	second := first.nextAttempt()
	_, err = sdkgo.RunMutation(second, transientClient.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry, "the 503 was not processed, so this attempt is still a first send")
	require.Contains(t, string(second.recordedHeartbeat), `"state":"notSent"`)
	result, err := sdkgo.RunMutation(second.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())

	terminalClient, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: projectconfig.ErrObjectNotFound},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	result, err = sdkgo.RunMutation(first.nextAttempt(), terminalClient.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchDefect, result.Branch, "the 503 was not processed, so no send may have been applied")
	require.Equal(t, 2, provider.requestCount())
}

func TestCreateTicketCredentialFailureAfterAPossibleSendRecordsNothing(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) { dropConnection(t, response) })
	first := newFreshdeskDexContext("create-credential-after-send")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.Error(t, err)
	for name, credentialErr := range map[string]error{
		"transient": errors.New("temporary credential store failure"), "terminal": projectconfig.ErrObjectNotFound,
	} {
		client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: credentialErr},
			freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
		require.NoError(t, err)
		second := first.nextAttempt()
		result, err := sdkgo.RunMutation(second, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
		if name == "transient" {
			require.Error(t, err, name)
		} else {
			require.NoError(t, err, name)
			require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "defect would hide the earlier possible send")
		}
		require.Nil(t, second.recordedHeartbeat, "%s: notSent would hide the earlier possible send", name)
	}
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketRetriesTransientCredentialReadBeforeSending(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1201, 2, nil))
	})
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: errors.New("temporary credential store failure")},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	first := newFreshdeskDexContext("create-transient-credential")
	_, err = sdkgo.RunMutation(first, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Contains(t, string(first.recordedHeartbeat), `"state":"notSent"`)
	require.Zero(t, provider.requestCount())
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketPermitsNextAttemptAfterFailedDispatchCheckpoint(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1201, 2, nil))
	})
	first := newFreshdeskDexContext("create-checkpoint-failure")
	first.rejectNextHeartbeat = true
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Contains(t, string(first.recordedHeartbeat), `"state":"notSent"`)
	require.Zero(t, provider.requestCount())
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketCountsALostClaimAfterAStaleCanResendMarker(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"code":"service_unavailable"}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1204, 2, nil))
	})
	first := newFreshdeskDexContext("create-503")
	_, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Contains(t, string(first.recordedHeartbeat), `"state":"canResend"`)

	second := first.nextAttempt()
	result, err := sdkgo.RunMutation(second, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())

	third := second.nextAttempt()
	third.previousHeartbeat = first.recordedHeartbeat
	_, err = sdkgo.RunMutation(third, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err, "the stale marker counts the lost claim, so one more send fits under the cap")
	require.Equal(t, 3, provider.requestCount())

	atCap := third.nextAttempt().nextAttempt()
	atCap.previousHeartbeat = first.recordedHeartbeat
	result, err = sdkgo.RunMutation(atCap, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "the stale marker counts every later attempt as a possible send")
	require.Equal(t, 3, provider.requestCount())
}

type shortenedAttemptCredentialProvider struct {
	stepContext *freshdeskDexContext
	cancel      context.CancelFunc
}

func (provider *shortenedAttemptCredentialProvider) Resolve(call sdkgo.Call) (freshdesk.Credentials, error) {
	credentials, err := testCredentialProvider().Resolve(call)
	provider.stepContext.Context, provider.cancel = context.WithTimeout(context.Background(), 2500*time.Millisecond)
	return credentials, err
}

func TestCreateTicketSkipsRequestWithoutAttemptTime(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, ticketBodyJSON(1201, 2, nil))
	})
	stepContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ctx := newFreshdeskDexContext("create-deadline")
	ctx.Context = stepContext
	credentials := &shortenedAttemptCredentialProvider{stepContext: ctx}
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, credentials,
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	defer credentials.cancel()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"canResend"`, "the request was not sent")
	require.Zero(t, provider.requestCount())
	next := ctx.nextAttempt()
	next.Context = context.Background()
	result, err := sdkgo.RunMutation(next, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

type untracedDialFailureTransport struct{ requests int }

func (transport *untracedDialFailureTransport) RoundTrip(*http.Request) (*http.Response, error) {
	transport.requests++
	return nil, &net.OpError{Op: "dial", Err: errors.New("untraced dial failure")}
}

func TestCreateTicketCountsAnUntracedDialErrorAsAPossibleSend(t *testing.T) {
	transport := &untracedDialFailureTransport{}
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, testCredentialProvider(),
		freshdesk.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	ctx := newFreshdeskDexContext("create-untraced")
	_, err = sdkgo.RunMutation(ctx, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Contains(t, string(ctx.recordedHeartbeat), `"state":"claimed"`, "an unproven failure keeps the claim")
	second := ctx.nextAttempt()
	_, err = sdkgo.RunMutation(second, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry)
	result, err := sdkgo.RunMutation(second.nextAttempt(), client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, 3, transport.requests)
}

func TestCreateTicketCutsAnAcceptedBodyBeforeTheAttemptDeadlineAndSelectsUncertain(t *testing.T) {
	provider := newRecordingFreshdesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		response.Header().Set("Content-Length", "200")
		response.WriteHeader(http.StatusCreated)
		_, err := io.WriteString(response, "{")
		require.NoError(t, err)
		response.(http.Flusher).Flush()
		<-request.Context().Done()
	})
	stepContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	first := newFreshdeskDexContext("create-accepted-body-cut")
	first.Context = stepContext
	result, err := sdkgo.RunMutation(first, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "a 201 whose body broke was accepted, so it is never resent")
	require.NoError(t, stepContext.Err(), "the result returned before the Step deadline")
	require.Contains(t, string(first.recordedHeartbeat), `"state":"claimed"`, "the cut-off send counts toward the cap")
	require.Equal(t, 1, provider.requestCount())
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
