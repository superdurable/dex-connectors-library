// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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
		{http.StatusCreated, `{"resource":{"booking_url":"javascript:alert(1)"}}`, calendly.CreateSchedulingLinkBranchUncertain, sdkgo.FailureProtocol},
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

func TestCreateSchedulingLinkRetriesOnlyRequestsCalendlyDidNotProcess(t *testing.T) {
	throttled := newFakeCalendly(t, map[string]http.HandlerFunc{"POST /scheduling_links": func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("X-RateLimit-Reset", "30")
		writeJSON(response, http.StatusTooManyRequests, providerError("Too Many Requests", "slow"))
	}})
	_, err := createSchedulingLink(t, newTestClient(t, throttled.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{}), testEventTypeURI)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter, "a 429 created nothing")
	require.Equal(t, 30*time.Second, retryAfter.After)

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

	almostExpired, cancel := context.WithTimeout(context.Background(), 4*time.Second)
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
