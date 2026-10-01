// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testEventURI = "https://api.calendly.com/scheduled_events/EVENT0001"

var testWindowStart = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

func TestListScheduledEventsScopesToTheConnectedUserAndReturnsTheNextPage(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /users/me": currentUserRoute(),
		"GET /scheduled_events": respondJSON(http.StatusOK, fmt.Sprintf(`{"collection":[%s,%s],"pagination":{"count":2,"next_page_token":"tok_next-2","next_page":"https://api.calendly.com/scheduled_events?page_token=tok_next-2"}}`,
			scheduledEventJSON("EVENT0001", "active", testWindowStart.Add(2*time.Hour)), scheduledEventJSON("EVENT0002", "canceled", testWindowStart.Add(4*time.Hour)))),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	pacific := time.FixedZone("PDT", -7*60*60)

	result, err := sdkgo.RunQuery(newStepContext("list"), client.ListScheduledEvents(), testConnection, calendly.ListScheduledEventsInput{
		MinStartTime: testWindowStart.In(pacific), MaxStartTime: testWindowStart.Add(7 * 24 * time.Hour), Status: calendly.ScheduledEventStatusActive,
		PageSize: 50, PageToken: "tok_first",
	})
	require.NoError(t, err)
	require.Equal(t, calendly.ListScheduledEventsBranchListed, result.Branch)
	require.Equal(t, "tok_next-2", result.Value.NextPageToken)
	require.Equal(t, testUserURI, result.Value.UserURI, "the page names the connected user it listed")
	require.Equal(t, testWindowStart, result.Value.MinStartTime, "the checked window is echoed in UTC")
	require.Len(t, result.Value.Events, 2)
	event := result.Value.Events[0]
	require.Equal(t, calendly.ScheduledEvent{
		URI: testEventURI, Name: "30 Minute Meeting", Status: "active",
		StartTime: testWindowStart.Add(2 * time.Hour), EndTime: testWindowStart.Add(150 * time.Minute), EventTypeURI: testEventTypeURI,
		Location:        calendly.ScheduledEventLocation{Type: "zoom", JoinURL: "https://zoom.us/j/123", Status: "pushed"},
		InviteesCounter: calendly.ScheduledEventInviteesCounter{Total: 2, Active: 1, Limit: 1},
		Hosts:           []calendly.ScheduledEventHost{{UserURI: testUserURI, Email: "host@example.com", Name: "Hana Host"}},
		CreatedAt:       time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, time.September, 2, 10, 0, 0, 0, time.UTC),
	}, event)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, dropped := range []string{"ZOOM-PASSWORD", "INTERNAL-NOTES", "guest@example.com"} {
		require.NotContains(t, string(encoded), dropped)
	}
	requireSecretFree(t, result)

	list := fake.requestsTo(http.MethodGet, "/scheduled_events")
	require.Len(t, list, 1)
	require.Equal(t, "Bearer "+sentinelToken, list[0].authorization)
	require.Equal(t, map[string][]string{
		"user": {testUserURI}, "min_start_time": {"2026-10-01T00:00:00.000000Z"}, "max_start_time": {"2026-10-08T00:00:00.000000Z"},
		"status": {"active"}, "sort": {"start_time:asc"}, "count": {"50"}, "page_token": {"tok_first"},
	}, map[string][]string(list[0].query))
}

func TestListScheduledEventsUsesTheNamedOrganizationWithoutReadingTheCurrentUser(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events": respondJSON(http.StatusOK, `{"collection":[],"pagination":{"count":0,"next_page_token":null}}`),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	result, err := sdkgo.RunQuery(newStepContext("list"), client.ListScheduledEvents(), testConnection, calendly.ListScheduledEventsInput{
		OrganizationURI: testOrganizationURI, MinStartTime: testWindowStart, MaxStartTime: testWindowStart.Add(time.Hour),
		Sort: calendly.ScheduledEventSortStartTimeDescending,
	})
	require.NoError(t, err)
	require.Equal(t, calendly.ListScheduledEventsBranchListed, result.Branch)
	require.Empty(t, result.Value.Events)
	require.Empty(t, result.Value.NextPageToken)
	require.Empty(t, fake.requestsTo(http.MethodGet, "/users/me"))
	query := fake.requestsTo(http.MethodGet, "/scheduled_events")[0].query
	require.Equal(t, testOrganizationURI, query.Get("organization"))
	require.Equal(t, "start_time:desc", query.Get("sort"))
	require.Empty(t, query.Get("user"))
}

func TestListScheduledEventsSelectsDefectWithoutCallingCalendly(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	window := calendly.ListScheduledEventsInput{MinStartTime: testWindowStart, MaxStartTime: testWindowStart.Add(time.Hour)}
	for _, test := range []struct {
		name           string
		mutate         func(*calendly.ListScheduledEventsInput)
		expectedReason string
	}{
		{"no window", func(input *calendly.ListScheduledEventsInput) { input.MinStartTime = time.Time{} }, "requires both"},
		{"reversed window", func(input *calendly.ListScheduledEventsInput) { input.MaxStartTime = input.MinStartTime }, "after minStartTime"},
		{"foreign user", func(input *calendly.ListScheduledEventsInput) { input.UserURI = "https://evil.example/users/X" }, "user URI"},
		{"bad organization", func(input *calendly.ListScheduledEventsInput) { input.OrganizationURI = testUserURI }, "organization URI"},
		{"bad status", func(input *calendly.ListScheduledEventsInput) { input.Status = "deleted" }, "status"},
		{"bad sort", func(input *calendly.ListScheduledEventsInput) { input.Sort = "name:asc" }, "sort"},
		{"bad email", func(input *calendly.ListScheduledEventsInput) { input.InviteeEmail = "Ada <ada@example.com>" }, "inviteeEmail"},
		{"page too large", func(input *calendly.ListScheduledEventsInput) { input.PageSize = 101 }, "pageSize"},
		{"bad page token", func(input *calendly.ListScheduledEventsInput) { input.PageToken = "a b" }, "pageToken"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := window
			test.mutate(&input)
			result, err := sdkgo.RunQuery(newStepContext("list"), client.ListScheduledEvents(), testConnection, input)
			require.NoError(t, err)
			require.Equal(t, calendly.ListScheduledEventsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.expectedReason)
		})
	}
	require.Empty(t, fake.recordedRequests())
}

func TestGetScheduledEventMapsCalendlyStatusesToBranches(t *testing.T) {
	for _, test := range []struct {
		name           string
		status         int
		body           string
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{"found", http.StatusOK, `{"resource":` + scheduledEventJSON("EVENT0001", "active", testWindowStart) + `}`, calendly.GetScheduledEventBranchFound, ""},
		{"not found", http.StatusNotFound, providerError("Resource Not Found", "gone"), calendly.GetScheduledEventBranchNotFound, sdkgo.FailureNotFound},
		{"bad token", http.StatusUnauthorized, providerError("Unauthenticated", "bad"), calendly.GetScheduledEventBranchProviderRejected, sdkgo.FailureAuthentication},
		{"no scope", http.StatusForbidden, `{"title":"Insufficient scope","message":"This operation requires the scopes listed in the 'required_scopes' array.","required_scopes":["scheduled_events:read"]}`,
			calendly.GetScheduledEventBranchProviderRejected, sdkgo.FailureAuthorization},
		{"bad request", http.StatusBadRequest, providerError("Invalid Argument", "bad"), calendly.GetScheduledEventBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"malformed", http.StatusOK, `{"resource":{"uri":"https://api.calendly.com/scheduled_events/EVENT0001","status":"maybe"}}`,
			calendly.GetScheduledEventBranchInvalidResponse, sdkgo.FailureProtocol},
		{"another event", http.StatusOK, `{"resource":` + scheduledEventJSON("EVENT9999", "active", testWindowStart) + `}`,
			calendly.GetScheduledEventBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCalendly(t, map[string]http.HandlerFunc{"GET /scheduled_events/EVENT0001": respondJSON(test.status, test.body)})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
			result, err := sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
				calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			if test.expectedKind == "" {
				require.Nil(t, result.Failure)
				require.Equal(t, testEventURI, result.Value.URI)
				require.Equal(t, testEventURI, result.Receipt.ProviderObjectID)
			} else {
				require.Equal(t, test.expectedKind, result.Failure.Kind)
			}
			requireSecretFree(t, result)
		})
	}
}

func TestGetScheduledEventRetriesRateLimitsAndServerErrors(t *testing.T) {
	for _, test := range []struct {
		name          string
		status        int
		header        map[string]string
		expectedKind  sdkgo.FailureKind
		expectedDelay time.Duration
	}{
		{"rate limit reset", http.StatusTooManyRequests, map[string]string{"X-RateLimit-Reset": "42"}, sdkgo.FailureRateLimit, 42 * time.Second},
		{"retry after", http.StatusTooManyRequests, map[string]string{"Retry-After": "7", "X-RateLimit-Reset": "42"}, sdkgo.FailureRateLimit, 7 * time.Second},
		{"server error", http.StatusServiceUnavailable, nil, sdkgo.FailureAvailability, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCalendly(t, map[string]http.HandlerFunc{"GET /scheduled_events/EVENT0001": func(response http.ResponseWriter, _ *http.Request) {
				for name, value := range test.header {
					response.Header().Set(name, value)
				}
				writeJSON(response, test.status, providerError("Too Many Requests", "slow down"))
			}})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
			_, err := sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
				calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.expectedKind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), providerSecretMessage)
			var retryAfter *dex.RetryAfterError
			if test.expectedDelay == 0 {
				require.False(t, errors.As(err, &retryAfter))
			} else {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.expectedDelay, retryAfter.After)
			}
		})
	}
}

func TestGetScheduledEventRetriesAServerErrorWhateverItsBodySize(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001": respondJSON(http.StatusServiceUnavailable, `{"title":"`+strings.Repeat("x", 128<<10)+`"}`),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{MaxResponseBytes: 64})
	_, err := sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
		calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the status decides; only a 2xx body is held to maxResponseBytes")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestGetScheduledEventSelectsInvalidResponseForAnOversizedBody(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /scheduled_events/EVENT0001": respondJSON(http.StatusOK, `{"resource":`+scheduledEventJSON("EVENT0001", "active", testWindowStart)+`}`),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{MaxResponseBytes: 64})
	result, err := sdkgo.RunQuery(newStepContext("get"), client.GetScheduledEvent(), testConnection,
		calendly.GetScheduledEventInput{ScheduledEventURI: testEventURI})
	require.NoError(t, err)
	require.Equal(t, calendly.GetScheduledEventBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestCancelScheduledEventCancelsOnceAndTreatsAnAlreadyCanceledEventAsCanceled(t *testing.T) {
	isCanceled := false
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /scheduled_events/EVENT0001/cancellation": func(response http.ResponseWriter, _ *http.Request) {
			if isCanceled {
				writeJSON(response, http.StatusForbidden, `{"title":"Permission Denied","message":"Event is already canceled"}`)
				return
			}
			isCanceled = true
			writeJSON(response, http.StatusCreated, `{"resource":{"canceled_by":"Hana Host","reason":"Conflict","canceler_type":"host","created_at":"2026-09-30T12:00:00.000000Z"}}`)
		},
		"GET /scheduled_events/EVENT0001": func(response http.ResponseWriter, _ *http.Request) {
			event := scheduledEventJSON("EVENT0001", "canceled", testWindowStart)
			writeJSON(response, http.StatusOK, `{"resource":`+event[:len(event)-1]+`,"cancellation":{"canceled_by":"Hana Host","reason":"Conflict","canceler_type":"host","created_at":"2026-09-30T12:00:00.000000Z"}}}`)
		},
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	input := calendly.CancelScheduledEventInput{ScheduledEventURI: testEventURI, Reason: "Conflict"}
	expectedCancellation := calendly.Cancellation{CanceledBy: "Hana Host", Reason: "Conflict", CancelerType: "host", CreatedAt: fixedNow}

	first, err := sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CancelScheduledEventBranchCanceled, first.Branch)
	require.False(t, first.Value.AlreadyCanceled)
	require.Equal(t, expectedCancellation, first.Value.Cancellation)

	repeated, err := sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendly.CancelScheduledEventBranchCanceled, repeated.Branch, "a duplicate dispatch reaches the same outcome")
	require.True(t, repeated.Value.AlreadyCanceled)
	require.Equal(t, expectedCancellation, repeated.Value.Cancellation)
	requireSecretFree(t, repeated)

	posts := fake.requestsTo(http.MethodPost, "/scheduled_events/EVENT0001/cancellation")
	require.Len(t, posts, 2)
	require.JSONEq(t, `{"reason":"Conflict"}`, posts[0].body)
}

func TestCancelScheduledEventRejectsAnActiveEventThatCalendlyRefuses(t *testing.T) {
	for _, test := range []struct {
		name            string
		refusal         string
		startTime       time.Time
		expectedKind    sdkgo.FailureKind
		expectedMessage string
	}{
		{"not allowed", `{"title":"Permission Denied","message":"You are not allowed to cancel this event"}`, fixedNow.Add(time.Hour), sdkgo.FailureAuthorization, "may not cancel"},
		{"in the past", `{"title":"Permission Denied","message":"Event in the past"}`, fixedNow.Add(-time.Hour), sdkgo.FailureConflict, "already started"},
		{"no scope", `{"title":"Insufficient scope","message":"x","required_scopes":["scheduled_events:write"]}`, fixedNow.Add(time.Hour), sdkgo.FailureAuthorization, "scheduled_events:write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeCalendly(t, map[string]http.HandlerFunc{
				"POST /scheduled_events/EVENT0001/cancellation": respondJSON(http.StatusForbidden, test.refusal),
				"GET /scheduled_events/EVENT0001":               respondJSON(http.StatusOK, `{"resource":`+scheduledEventJSON("EVENT0001", "active", test.startTime)+`}`),
			})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
			result, err := sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection,
				calendly.CancelScheduledEventInput{ScheduledEventURI: testEventURI})
			require.NoError(t, err)
			require.Equal(t, calendly.CancelScheduledEventBranchProviderRejected, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.expectedMessage)
			require.JSONEq(t, `{}`, fake.requestsTo(http.MethodPost, "/scheduled_events/EVENT0001/cancellation")[0].body)
		})
	}
}

func TestCancelScheduledEventSelectsNotFoundAndRetriesALostAnswer(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"POST /scheduled_events/EVENT0002/cancellation": respondJSON(http.StatusNotFound, providerError("Resource Not Found", "gone")),
		"POST /scheduled_events/EVENT0003/cancellation": respondJSON(http.StatusBadGateway, providerError("Bad Gateway", "oops")),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	missing, err := sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection,
		calendly.CancelScheduledEventInput{ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVENT0002"})
	require.NoError(t, err)
	require.Equal(t, calendly.CancelScheduledEventBranchNotFound, missing.Branch)

	_, err = sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection,
		calendly.CancelScheduledEventInput{ScheduledEventURI: "https://api.calendly.com/scheduled_events/EVENT0003"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a repeated cancellation is harmless, so a server error retries")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)

	tooLong, err := sdkgo.RunMutation(newStepContext("cancel"), client.CancelScheduledEvent(), testConnection,
		calendly.CancelScheduledEventInput{ScheduledEventURI: testEventURI, Reason: string(make([]rune, 10001))})
	require.NoError(t, err)
	require.Equal(t, calendly.CancelScheduledEventBranchDefect, tooLong.Branch)
	require.Contains(t, tooLong.Failure.Message, strconv.Itoa(10000))
}
