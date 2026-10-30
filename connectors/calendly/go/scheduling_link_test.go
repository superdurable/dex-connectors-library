// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createdLinkBody = `{"resource":{"booking_url":"https://calendly.com/d/abcd-1234/30-minute-meeting","owner":"https://api.calendly.com/event_types/TYPE0001","owner_type":"EventType"}}`

func createSchedulingLink(t *testing.T, client *calendly.Client, eventTypeURI string) (calendly.CreateSchedulingLinkResult, error) {
	t.Helper()
	return sdkgo.RunMutation(newStepContext("link"), client.CreateSchedulingLink(), testConnection,
		calendly.CreateSchedulingLinkInput{EventTypeURI: eventTypeURI})
}

func TestCreateSchedulingLinkPostsOneSingleUseLink(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	result, err := createSchedulingLink(t, client, testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, result.Branch)
	require.Equal(t, calendly.SchedulingLink{BookingURL: "https://calendly.com/d/abcd-1234/30-minute-meeting", EventTypeURI: testEventTypeURI}, result.Value)
	require.Equal(t, result.Value.BookingURL, result.Receipt.ProviderObjectID)
	posts := fake.requestsTo(http.MethodPost, "/scheduling_links")
	require.Len(t, posts, 1)
	require.JSONEq(t, `{"max_event_count":1,"owner":"https://api.calendly.com/event_types/TYPE0001","owner_type":"EventType"}`, posts[0].body)
	require.Equal(t, "Bearer "+sentinelToken, posts[0].authorization)
	requireSecretFree(t, result)
}

func TestCreateSchedulingLinkMapsResponsesWithoutRepeatingAPossiblyCreatedLink(t *testing.T) {
	for _, test := range []struct {
		status         int
		body           string
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{http.StatusBadRequest, providerError("Invalid Argument", "owner"), calendly.CreateSchedulingLinkBranchProviderRejected, sdkgo.FailureProviderRejection},
		{http.StatusForbidden, providerError("Permission Denied", "You do not have permission"), calendly.CreateSchedulingLinkBranchProviderRejected, sdkgo.FailureAuthorization},
		{http.StatusNotFound, providerError("Resource Not Found", "type"), calendly.CreateSchedulingLinkBranchProviderRejected, sdkgo.FailureNotFound},
		{http.StatusInternalServerError, providerError("Internal Server Error", "boom"), calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureAvailability},
		{http.StatusConflict, providerError("Conflict", "existing"), calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureConflict},
		{http.StatusCreated, `{"resource":{"booking_url":"javascript:alert(1)"}}`, calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureProtocol},
		{http.StatusAccepted, `{}`, calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureProtocol},
		{http.StatusNoContent, `{}`, calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureProtocol},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(test.status, test.body)})
			result, err := createSchedulingLink(t, newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
			requireSecretFree(t, result)
		})
	}
}

func TestCreateSchedulingLinkAcceptsDecodable202(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusAccepted, createdLinkBody)})
	result, err := createSchedulingLink(t, newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

func TestCreateSchedulingLinkRetriesOnlyRequestsCalendlyDidNotProcess(t *testing.T) {
	throttled := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-RateLimit-Reset", "30")
		writeJSON(response, http.StatusTooManyRequests, providerError("Too Many Requests", "slow"))
	}})
	_, err := createSchedulingLink(t, newTestClient(t, throttled.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter, "a 429 created nothing")
	require.Equal(t, 30*time.Second, retryAfter.After)
	serviceUnavailable := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusServiceUnavailable, providerError("Unavailable", "later"))})
	_, err = createSchedulingLink(t, newTestClient(t, serviceUnavailable.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	var unavailableRetry *sdkgo.RetryError
	require.ErrorAs(t, err, &unavailableRetry, "a 503 proves Calendly did not create a link")
	require.Equal(t, sdkgo.FailureAvailability, unavailableRetry.Failure.Kind)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	refused := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		redirected := request.Clone(request.Context())
		redirected.URL.Host, redirected.Host = closedAddress, closedAddress
		return http.DefaultTransport.RoundTrip(redirected)
	})}
	_, err = createSchedulingLink(t, newTestClient(t, refused, personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refused connection never sent the request")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	hanging := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(_ http.ResponseWriter, request *http.Request) {
		_, _ = io.ReadAll(request.Body)
		<-request.Context().Done()
	}})
	impatient := hanging.redirectingClient()
	impatient.Timeout = 200 * time.Millisecond
	result, err := createSchedulingLink(t, newTestClient(t, impatient, personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, result.Branch, "Calendly read the request and may have created a link")
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Len(t, hanging.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

func TestCreateSchedulingLinkKeepsThePostInsideTheExecuteDeadline(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})

	almostExpired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := sdkgo.RunMutation(&stepContext{Context: almostExpired, step: "link"}, client.CreateSchedulingLink(), testConnection,
		calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "nothing was sent, so a Retry is safe")
	require.Empty(t, fake.requestsTo(http.MethodPost, "/scheduling_links"))

	shortAttempt, cancelShort := context.WithTimeout(context.Background(), 9*time.Second)
	defer cancelShort()
	started := time.Now()
	result, err := sdkgo.RunMutation(&stepContext{Context: shortAttempt, step: "link"}, client.CreateSchedulingLink(), testConnection,
		calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI})
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, result.Branch, "the POST gave up before the attempt deadline")
	require.Less(t, time.Since(started), 8*time.Second)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

func TestCreateSchedulingLinkSelectsDefectForAnInvalidEventType(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	for _, eventTypeURI := range []string{"", "TYPE0001", "https://api.calendly.com/users/USER0001", "https://example.com/event_types/TYPE0001"} {
		result, err := createSchedulingLink(t, client, eventTypeURI)
		require.NoError(t, err)
		require.Equal(t, calendly.CreateSchedulingLinkBranchDefect, result.Branch, eventTypeURI)
	}
	require.Empty(t, fake.recordedRequests())
}

func TestCreateSchedulingLinkRunsWithSyncDurability(t *testing.T) {
	require.Equal(t, dex.StepDurabilitySync, calendly.CreateSchedulingLinkDefinition.StepDefaults.ExecuteDurability,
		"Calendly has no idempotency key, so a duplicate async dispatch would create a second link")
	for _, definition := range []sdkgo.MutationDefinition{calendly.CancelScheduledEventDefinition, calendly.CreateWebhookSubscriptionDefinition} {
		require.Equal(t, dex.StepDurabilityAsync, definition.StepDefaults.ExecuteDurability, definition.Operation.OperationID)
	}
}

func TestCreateSchedulingLinkFenceRetries503ThenNeverResendsAcceptedLink(t *testing.T) {
	var requests atomic.Int32
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(response http.ResponseWriter, request *http.Request) {
		if requests.Add(1) == 1 {
			writeJSON(response, http.StatusServiceUnavailable, providerError("Unavailable", "retry"))
			return
		}
		respondJSON(http.StatusCreated, createdLinkBody)(response, request)
	}})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("link-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	_, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, result.Branch)
	ctx.AdvanceAttempt()
	replayed, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, replayed.Branch,
		"Calendly has no provider read-back for a previous accepted single-use link")
	require.Equal(t, int32(2), requests.Load(), "an accepted link is never posted twice")
}

func TestCreateSchedulingLinkFenceFailsClosedAfterLostClaim(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		require.Nil(t, request.GetBody, "the HTTP transport must not replay a fenced write")
		return nil, errors.New("response lost after possible acceptance")
	})}, personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("lost-flow", "create-link")
	ctx.LoseNextHeartbeat()
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	first, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, second.Branch)
	require.Equal(t, int32(1), requests.Load())
}

func TestCreateSchedulingLinkFenceRejectsStaleResendPermission(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusServiceUnavailable, providerError("Unavailable", "retry"))})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("stale-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	_, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

type unavailableOnceCredentialSource struct {
	requests atomic.Int32
}

func (source *unavailableOnceCredentialSource) Resolve(sdkgo.Call) (calendly.Credentials, error) {
	if source.requests.Add(1) == 1 {
		return calendly.Credentials{}, errors.New("credential broker unavailable")
	}
	return personalAccessTokenCredentials("")[testConnection], nil
}

func (source *unavailableOnceCredentialSource) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[calendly.Credentials]) (calendly.Credentials, error) {
	return source.Resolve(call)
}

func TestCreateSchedulingLinkFenceRecordsCredentialFailureBeforeSending(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	source := &unavailableOnceCredentialSource{}
	client := newTestClient(t, fake.redirectingClient(), source, calendly.Config{})
	ctx := singledispatchtest.NewContext("credential-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	_, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, fake.requestsTo(http.MethodPost, "/scheduling_links"))
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

func TestCreateSchedulingLinkReauthorizationSelectsDefectBeforeSending(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /oauth/token":      respondJSON(http.StatusBadRequest, `{"error":"invalid_grant"}`),
		"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody),
	})
	result, err := createSchedulingLink(t, newRefreshingClient(t, expiredOAuthCredentials(), fake.redirectingClient()), testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "reauthorization")
	require.Empty(t, fake.requestsTo(http.MethodPost, "/scheduling_links"))
}

func TestCreateSchedulingLinkRetriesWithoutSendingWhenClaimCheckpointFails(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("claim-failure-flow", "create-link")
	ctx.SetWriteError(errors.New("checkpoint unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection,
		calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, fake.requestsTo(http.MethodPost, "/scheduling_links"))
}

func TestCreateSchedulingLinkFailsClosedWhenCheckpointReadFails(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("read-failure-flow", "create-link")
	ctx.SetReadError(errors.New("checkpoint unavailable"))
	result, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection,
		calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI})
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, result.Branch)
	require.Empty(t, fake.requestsTo(http.MethodPost, "/scheduling_links"))
}

func TestCreateSchedulingLinkResendsOnceAfterProvenUndispatchedAttempt(t *testing.T) {
	var attempts atomic.Int32
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newTestClient(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			redirected := request.Clone(request.Context())
			redirected.URL.Host, redirected.Host = closedAddress, closedAddress
			return http.DefaultTransport.RoundTrip(redirected)
		}
		return fake.redirectingClient().Transport.RoundTrip(request)
	})}, personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("undispatched-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	_, err = sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchCreated, result.Branch)
	require.Equal(t, int32(2), attempts.Load())
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

type interruptedResponseBody struct{}

func (interruptedResponseBody) Read([]byte) (int, error) {
	return 0, errors.New("connection closed during accepted response")
}

func TestCreateSchedulingLinkNeverRetriesAnUnreadableAcceptedResponse(t *testing.T) {
	var requests atomic.Int32
	client := newTestClient(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{}, Body: io.NopCloser(interruptedResponseBody{})}, nil
	})}, personalAccessTokenCredentials(""), calendly.Config{})
	ctx := singledispatchtest.NewContext("unreadable-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	first, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, first.Branch)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, second.Branch)
	require.Equal(t, int32(1), requests.Load())
}

func TestCreateSchedulingLinkNeverRetriesAnOversizedAcceptedResponse(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": respondJSON(http.StatusCreated, createdLinkBody)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{MaxResponseBytes: 32})
	ctx := singledispatchtest.NewContext("oversized-flow", "create-link")
	input := calendly.CreateSchedulingLinkInput{EventTypeURI: testEventTypeURI}
	first, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, first.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, first.Failure.Kind)
	ctx.AdvanceAttempt()
	second, err := sdkgo.RunMutation(ctx, client.CreateSchedulingLink(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, second.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/scheduling_links"), 1)
}

func TestCreateSchedulingLinkDoesNotReplayOnReusedConnectionAfterLostReply(t *testing.T) {
	var posts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/prime" {
			response.WriteHeader(http.StatusOK)
			return
		}
		posts.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		connection, _, err := response.(http.Hijacker).Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer provider.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	prime, err := http.NewRequest(http.MethodGet, provider.URL+"/prime", nil)
	require.NoError(t, err)
	primed, err := transport.RoundTrip(prime)
	require.NoError(t, err)
	require.NoError(t, primed.Body.Close())
	target, err := url.Parse(provider.URL)
	require.NoError(t, err)
	client := newTestClient(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Nil(t, request.GetBody)
		redirected := request.Clone(request.Context())
		redirected.URL.Scheme, redirected.URL.Host, redirected.Host = target.Scheme, target.Host, target.Host
		redirected.Header.Set("Idempotency-Key", "transport-replay-probe")
		return transport.RoundTrip(redirected)
	})}, personalAccessTokenCredentials(""), calendly.Config{})
	result, err := createSchedulingLink(t, client, testEventTypeURI)
	require.NoError(t, err)
	require.Equal(t, calendly.CreateSchedulingLinkBranchUncertain, result.Branch)
	require.Equal(t, int32(1), posts.Load())
}
