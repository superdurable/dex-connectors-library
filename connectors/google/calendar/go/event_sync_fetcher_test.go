// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/go/internal/calendarsyncfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
)

const syncCalendarID = "team@group.calendar.google.com"

func newSyncTestFetcher(t *testing.T, fake *calendarsyncfake.Calendar, calendarID string) *eventSyncFetcher {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: "calendar-sync"}
	client, err := New(Config{Endpoint: fake.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	})
	require.NoError(t, err)
	return newEventSyncFetcher(client, reference, EventChangedTriggerConfiguration{CalendarID: calendarID})
}

func decodeSyncCursor(t *testing.T, cursor pollingtrigger.Cursor) eventSyncCursor {
	t.Helper()
	var position eventSyncCursor
	require.NoError(t, json.Unmarshal([]byte(cursor.Token), &position))
	return position
}

func TestEventSyncBaselinePagesThroughTheFullSyncAndKeepsOnlyTheSyncToken(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	for index := range 260 {
		fake.Insert("existing"+strconv.Itoa(index), "Existing")
	}
	fetcher := newSyncTestFetcher(t, fake, syncCalendarID)

	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)

	require.Equal(t, eventSyncCursor{CalendarID: syncCalendarID, SyncToken: "sync-260"}, decodeSyncCursor(t, cursor))
	requests := fake.Requests()
	require.Len(t, requests, 2)
	require.Equal(t, "maxResults=250", requests[0].Query.Encode(), "the full sync sends no filter it could not repeat")
	require.Equal(t, "maxResults=250&pageToken=page-250-260", requests[1].Query.Encode())
}

func TestEventSyncFetchReportsCreatedUpdatedAndCancelledEventsWithStableIDs(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	fake.Insert("standup0001", "Standup")
	fake.Insert("review00001", "Review")
	fetcher := newSyncTestFetcher(t, fake, syncCalendarID)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.Insert("kickoff0001", "Kickoff")
	fake.Update("standup0001", "Standup moved")
	fake.Cancel("review00001")

	page, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)

	require.False(t, page.HasMorePages)
	require.Len(t, page.Changes, 3)
	kinds := []EventChangeKind{}
	for _, change := range page.Changes {
		require.Equal(t, syncCalendarID, change.Payload.CalendarID)
		kinds = append(kinds, change.Payload.ChangeKind)
	}
	require.Equal(t, []EventChangeKind{EventChangeCreated, EventChangeUpdated, EventChangeCancelled}, kinds)
	require.Equal(t, "Kickoff", page.Changes[0].Payload.Event.Summary)
	require.Equal(t, "Standup moved", page.Changes[1].Payload.Event.Summary)
	require.Equal(t, "review00001", page.Changes[2].Payload.Event.ID)
	require.Equal(t, time.Date(2026, 3, 2, 9, 4, 0, 0, time.UTC), page.Changes[1].OccurredAt)
	require.Equal(t, eventSyncCursor{CalendarID: syncCalendarID, SyncToken: "sync-5"}, decodeSyncCursor(t, page.Next))
	last := fake.Requests()[len(fake.Requests())-1]
	require.Equal(t, "maxResults=250&syncToken=sync-2", last.Query.Encode())

	refetched, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	for index := range page.Changes {
		require.Equal(t, page.Changes[index].ID, refetched.Changes[index].ID, "a refetch after a crash yields the same Trigger event IDs")
	}
	require.NotEqual(t, page.Changes[0].ID, page.Changes[1].ID)
}

func TestEventSyncFetchRepeatsTheSyncTokenWithEachPageToken(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	fetcher := newSyncTestFetcher(t, fake, syncCalendarID)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	for index := range 260 {
		fake.Insert("burst"+strconv.Itoa(index), "Burst")
	}

	first, err := fetcher.FetchChanges(context.Background(), cursor)
	require.NoError(t, err)
	require.True(t, first.HasMorePages)
	require.Len(t, first.Changes, 250)
	require.Equal(t, eventSyncCursor{CalendarID: syncCalendarID, SyncToken: "sync-0", PageToken: "page-250-260"}, decodeSyncCursor(t, first.Next))
	second, err := fetcher.FetchChanges(context.Background(), first.Next)
	require.NoError(t, err)

	require.False(t, second.HasMorePages)
	require.Len(t, second.Changes, 10)
	require.Equal(t, eventSyncCursor{CalendarID: syncCalendarID, SyncToken: "sync-260"}, decodeSyncCursor(t, second.Next))
	last := fake.Requests()[len(fake.Requests())-1]
	require.Equal(t, "maxResults=250&pageToken=page-250-260&syncToken=sync-0", last.Query.Encode())
}

func TestEventSyncFetchMarksAnExpiredOrForeignCursorInvalid(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	fetcher := newSyncTestFetcher(t, fake, syncCalendarID)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.ExpireSyncTokens()

	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "410 Gone requires a new full sync: %v", err)

	other := newSyncTestFetcher(t, fake, "primary")
	_, err = other.FetchChanges(context.Background(), cursor)
	require.True(t, pollingtrigger.IsCursorInvalid(err), "a binding moved to another calendar records a new baseline")
	_, err = fetcher.FetchChanges(context.Background(), pollingtrigger.Cursor{Token: "not-json"})
	require.True(t, pollingtrigger.IsCursorInvalid(err))
}

func TestEventSyncFetchReportsRateLimitsAndOtherFailuresWithoutResetting(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	fetcher := newSyncTestFetcher(t, fake, syncCalendarID)
	cursor, err := fetcher.SeedCursor(context.Background())
	require.NoError(t, err)
	fake.RateLimitNextRequests(1, "7")

	_, err = fetcher.FetchChanges(context.Background(), cursor)
	var rateLimited *pollingtrigger.RateLimitedError
	require.True(t, errors.As(err, &rateLimited), "%v", err)
	require.Equal(t, 7*time.Second, rateLimited.RetryAfter)
	require.False(t, pollingtrigger.IsCursorInvalid(err))

	for _, reason := range []string{"userRateLimitExceeded", "rateLimitExceeded", "quotaExceeded", "dailyLimitExceeded"} {
		fake.ThrottleNextRequests(1, http.StatusForbidden, reason, "")
		_, err = fetcher.FetchChanges(context.Background(), cursor)
		require.True(t, errors.As(err, &rateLimited), "403 %s pauses the connection: %v", reason, err)
		require.Zero(t, rateLimited.RetryAfter, "no Retry-After backs off exponentially")
	}
	// Google may send an HTTP date; the operation path rejects it, but a poll still pauses for it.
	fake.RateLimitNextRequests(1, time.Now().Add(time.Hour).UTC().Format(http.TimeFormat))
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.True(t, errors.As(err, &rateLimited), "%v", err)
	require.Greater(t, rateLimited.RetryAfter, 30*time.Minute)
	fake.ThrottleNextRequests(1, http.StatusForbidden, "forbidden", "")
	_, err = fetcher.FetchChanges(context.Background(), cursor)
	require.False(t, errors.As(err, &rateLimited), "a plain 403 backs off per binding: %v", err)
	require.False(t, pollingtrigger.IsCursorInvalid(err))

	missing := newSyncTestFetcher(t, fake, "deleted@group.calendar.google.com")
	_, err = missing.SeedCursor(context.Background())
	require.Error(t, err)
	require.False(t, pollingtrigger.IsCursorInvalid(err), "a missing calendar is retried, not reseeded")
	require.NotContains(t, err.Error(), "calendar-token")
}

func TestEventChangeKindIsDerivedFromCreatedAndUpdatedTimes(t *testing.T) {
	require.Equal(t, EventChangeCreated, eventChangeKind(Event{Status: "confirmed", Created: "2026-03-02T09:00:00.000Z", Updated: "2026-03-02T09:00:00.640Z"}))
	require.Equal(t, EventChangeUpdated, eventChangeKind(Event{Status: "confirmed", Created: "2026-03-02T09:00:00Z", Updated: "2026-03-02T09:00:02Z"}))
	require.Equal(t, EventChangeUpdated, eventChangeKind(Event{Status: "tentative"}), "missing timestamps are not a creation")
	require.Equal(t, EventChangeCancelled, eventChangeKind(Event{Status: "cancelled", Created: "2026-03-02T09:00:00Z", Updated: "2026-03-02T09:00:00Z"}))
}

func TestEventChangedTriggerConfigurationDefaultsToThePrimaryCalendar(t *testing.T) {
	require.Equal(t, DefaultTriggerCalendarID, EventChangedTriggerConfiguration{}.WatchedCalendarID())
	require.Equal(t, syncCalendarID, EventChangedTriggerConfiguration{CalendarID: syncCalendarID}.WatchedCalendarID())
	require.NoError(t, EventChangedTriggerConfiguration{}.Validate())
	require.Error(t, EventChangedTriggerConfiguration{CalendarID: " primary"}.Validate())
	_, err := New(Config{PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
}
