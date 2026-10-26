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
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
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
	require.JSONEq(t, `{"freshdeskDispatchedCallId":"`+string(result.Receipt.CallID)+`","freshdeskDispatchedAttempt":1}`, string(ctx.recordedHeartbeat),
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
			require.NoError(t, err, "ambiguous dispatched outcomes are not retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIKey)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
			if test.branch == freshdesk.CreateTicketBranchUncertain {
				replayed, replayErr := sdkgo.RunMutation(ctx.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
				require.NoError(t, replayErr)
				require.Equal(t, freshdesk.CreateTicketBranchUncertain, replayed.Branch)
				require.Equal(t, 1, provider.requestCount(), "an accepted or possibly accepted write is never resent")
			}
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"freshdeskRetryAttempt":2`, "a 429 permits only the next attempt")
	require.Equal(t, 2, ctx.heartbeatCount)

	refused := newFreshdeskDexContext("create-refused")
	_, err = sdkgo.RunMutation(refused, newFreshdeskClient(t, closedLoopbackURL(t)).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Contains(t, string(refused.recordedHeartbeat), `"freshdeskRetryAttempt":2`, "a refused connection permits only the next attempt")

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

	missing := newFreshdeskDexContext("create-missing-marker").nextAttempt()
	result, err = sdkgo.RunMutation(missing, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "an unacknowledged dispatch marker cannot authorize a resend")
	require.Zero(t, provider.requestCount())

	unrecordable := newFreshdeskDexContext("create-unrecordable")
	unrecordable.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(unrecordable, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
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
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketKeepsThePermitAfterCredentialResolutionFails(t *testing.T) {
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
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, failingFreshdeskCredentials{err: projectconfig.ErrObjectNotFound},
		freshdesk.WithAPIBaseURL(provider.URL+"/api/v2"))
	require.NoError(t, err)
	second := first.nextAttempt()
	_, err = sdkgo.RunMutation(second, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.ErrorAs(t, err, &retry, "a permitted later attempt cannot report a credential defect")
	require.Contains(t, string(second.recordedHeartbeat), `"freshdeskRetryAttempt":3`)
	require.Equal(t, 1, provider.requestCount())
	third := second.nextAttempt()
	result, err := sdkgo.RunMutation(third, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
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
	require.Contains(t, string(first.recordedHeartbeat), `"freshdeskRetryAttempt":2`)
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
	require.Contains(t, string(first.recordedHeartbeat), `"freshdeskRetryAttempt":2`)
	require.Zero(t, provider.requestCount())
	result, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateTicketRetriesServiceUnavailableOnlyOnTheNextAttempt(t *testing.T) {
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
	require.Contains(t, string(first.recordedHeartbeat), `"freshdeskRetryAttempt":2`)

	second := first.nextAttempt()
	result, err := sdkgo.RunMutation(second, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())

	third := second.nextAttempt()
	third.previousHeartbeat = first.recordedHeartbeat
	result, err = sdkgo.RunMutation(third, newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch, "a stale safe marker cannot resend an accepted ticket")
	require.Equal(t, 2, provider.requestCount())
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
	require.Contains(t, string(ctx.recordedHeartbeat), `"freshdeskRetryAttempt":2`)
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

func TestCreateTicketDoesNotRetryAnUntracedDialError(t *testing.T) {
	transport := &untracedDialFailureTransport{}
	client, err := freshdesk.New(freshdesk.Config{Domain: testDomain}, testCredentialProvider(),
		freshdesk.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	ctx := newFreshdeskDexContext("create-untraced")
	result, err := sdkgo.RunMutation(ctx, client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	replayed, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, replayed.Branch)
	require.Equal(t, 1, transport.requests)
}

func TestCreateTicketCutsAnAcceptedBodyBeforeTheAttemptDeadlineWithoutResending(t *testing.T) {
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
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, result.Branch)
	require.NoError(t, stepContext.Err(), "the result returned before the Step deadline")
	replayed, err := sdkgo.RunMutation(first.nextAttempt(), newFreshdeskClient(t, provider.URL).CreateTicket(), freshdeskConnection, validCreateTicketInput())
	require.NoError(t, err)
	require.Equal(t, freshdesk.CreateTicketBranchUncertain, replayed.Branch)
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
