// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func timedMeetingInput() calendar.CreateEventInput {
	return calendar.CreateEventInput{
		CalendarID:  "primary",
		Summary:     "Meridian kickoff",
		Description: "Agenda to follow.",
		Start:       calendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles"},
		End:         calendar.EventDateTime{DateTime: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles"},
		Attendees:   []calendar.EventAttendeeInput{{Email: "priya@meridian.example.com"}, {Email: "alice@example.com", IsOptional: true}},
	}
}

func TestCreateEventWritesUnderAStableDerivedEventID(t *testing.T) {
	store := newEventStore()
	provider := newRecordingProvider(t, store.serve(t))
	client := newCalendarClient(t, provider.URL)
	input := timedMeetingInput()
	input.RequestConference = true
	input.SendUpdates = "all"

	result, err := sdkgo.RunMutation(newCalendarDexContext("create-once"), client.CreateEvent(), calendarConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendar.CreateEventBranchCreated, result.Branch)
	require.False(t, result.Value.WasAlreadyCreated)

	eventID := strings.ReplaceAll(string(result.Receipt.CallID), "-", "")
	require.Len(t, eventID, 32)
	require.Equal(t, sdkgo.IdempotencyKey(eventID), result.Receipt.IdempotencyKey)
	require.Equal(t, eventID, result.Receipt.ProviderObjectID)
	require.Equal(t, eventID, result.Value.Event.ID)
	require.Equal(t, 2, provider.requestCount())

	preRead := provider.request(t, 0)
	require.Equal(t, http.MethodGet, preRead.method)
	require.Equal(t, "/calendars/primary/events/"+eventID, preRead.path)
	insert := provider.request(t, 1)
	require.Equal(t, http.MethodPost, insert.method)
	require.Equal(t, "/calendars/primary/events", insert.path)
	require.Equal(t, "1", insert.query.Get("conferenceDataVersion"))
	require.Equal(t, "all", insert.query.Get("sendUpdates"))
	body := decodeRequestBody(t, insert)
	require.Equal(t, eventID, body["id"])
	require.Equal(t, "Meridian kickoff", body["summary"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-24T09:00:00-08:00", "timeZone": "America/Los_Angeles"}, body["start"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-24T10:00:00-08:00", "timeZone": "America/Los_Angeles"}, body["end"])
	require.Equal(t, []any{
		map[string]any{"email": "priya@meridian.example.com"},
		map[string]any{"email": "alice@example.com", "optional": true},
	}, body["attendees"])
	require.Equal(t, map[string]any{"createRequest": map[string]any{
		"requestId": eventID, "conferenceSolutionKey": map[string]any{"type": "hangoutsMeet"},
	}}, body["conferenceData"])
}

func TestCreateEventRetriedInTheSameStepExecutionReturnsItsOwnEvent(t *testing.T) {
	store := newEventStore()
	provider := newRecordingProvider(t, store.serve(t))
	client := newCalendarClient(t, provider.URL)

	first, err := sdkgo.RunMutation(newCalendarDexContext("create-retried"), client.CreateEvent(), calendarConnection, timedMeetingInput())
	require.NoError(t, err)
	replay, err := sdkgo.RunMutation(newCalendarDexContext("create-retried"), client.CreateEvent(), calendarConnection, timedMeetingInput())
	require.NoError(t, err)

	require.Equal(t, calendar.CreateEventBranchCreated, replay.Branch)
	require.True(t, replay.Value.WasAlreadyCreated)
	require.Equal(t, first.Value.Event.ID, replay.Value.Event.ID)
	require.Equal(t, 1, store.insertCount())
	require.Equal(t, 3, provider.requestCount(), "the replay reads the stable ID and sends no insert")

	other, err := sdkgo.RunMutation(newCalendarDexContext("create-other-step-execution"), client.CreateEvent(), calendarConnection, timedMeetingInput())
	require.NoError(t, err)
	require.NotEqual(t, first.Value.Event.ID, other.Value.Event.ID, "a new Step execution is a new logical create")
}

func TestCreateEventReadsBackWhenGoogleReportsTheIDAsDuplicate(t *testing.T) {
	store := newEventStore()
	store.hidesEventsFromFirstRead = true
	provider := newRecordingProvider(t, store.serve(t))
	client := newCalendarClient(t, provider.URL)
	input := timedMeetingInput()
	input.EventID = "kickoffopp123"
	store.put(input.EventID, "confirmed")

	result, err := sdkgo.RunMutation(newCalendarDexContext("create-duplicate"), client.CreateEvent(), calendarConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.WasAlreadyCreated)
	require.Equal(t, "kickoffopp123", result.Value.Event.ID)
	require.Equal(t, sdkgo.IdempotencyKey("kickoffopp123"), result.Receipt.IdempotencyKey)
	require.Equal(t, []string{http.MethodGet, http.MethodPost, http.MethodGet}, provider.methods())
}

func TestCreateEventSelectsConflictForADeletedOrUnreadableID(t *testing.T) {
	t.Run("deleted event keeps its ID", func(t *testing.T) {
		store := newEventStore()
		provider := newRecordingProvider(t, store.serve(t))
		client := newCalendarClient(t, provider.URL)
		input := timedMeetingInput()
		input.EventID = "kickoffopp123"
		store.put(input.EventID, "cancelled")
		result, err := sdkgo.RunMutation(newCalendarDexContext("create-deleted"), client.CreateEvent(), calendarConnection, input)
		require.NoError(t, err)
		require.Equal(t, calendar.CreateEventBranchConflict, result.Branch)
		require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
		require.Zero(t, store.insertCount())
	})
	t.Run("duplicate that cannot be read", func(t *testing.T) {
		provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			if request.Method == http.MethodPost {
				writeJSON(t, response, http.StatusConflict, `{"error":{"errors":[{"reason":"duplicate"}]}}`)
				return
			}
			writeJSON(t, response, http.StatusNotFound, `{}`)
		})
		client := newCalendarClient(t, provider.URL)
		result, err := sdkgo.RunMutation(newCalendarDexContext("create-unreadable"), client.CreateEvent(), calendarConnection, timedMeetingInput())
		require.NoError(t, err)
		require.Equal(t, calendar.CreateEventBranchConflict, result.Branch)
	})
}

func TestCreateEventRetriesAmbiguousInsertsInsteadOfReportingUncertainty(t *testing.T) {
	for _, test := range []struct {
		name string
		kind sdkgo.FailureKind
		fail func(http.ResponseWriter)
	}{
		{name: "5xx after dispatch", kind: sdkgo.FailureAvailability, fail: func(response http.ResponseWriter) {
			response.WriteHeader(http.StatusServiceUnavailable)
		}},
		{name: "connection lost after dispatch", kind: sdkgo.FailureTransport, fail: func(response http.ResponseWriter) {
			connection, _, err := response.(http.Hijacker).Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newEventStore()
			provider := newRecordingProvider(t, store.serve(t))
			store.afterInsert = test.fail
			client := newCalendarClient(t, provider.URL)

			_, err := sdkgo.RunMutation(newCalendarDexContext("create-ambiguous"), client.CreateEvent(), calendarConnection, timedMeetingInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.Equal(t, 1, store.insertCount(), "Google stored the event although the response was lost")

			store.afterInsert = nil
			retried, err := sdkgo.RunMutation(newCalendarDexContext("create-ambiguous"), client.CreateEvent(), calendarConnection, timedMeetingInput())
			require.NoError(t, err)
			require.Equal(t, calendar.CreateEventBranchCreated, retried.Branch)
			require.True(t, retried.Value.WasAlreadyCreated)
			require.Equal(t, 1, store.insertCount())
		})
	}
}

func TestCreateEventSendsAllDayDatesWithoutAZone(t *testing.T) {
	store := newEventStore()
	provider := newRecordingProvider(t, store.serve(t))
	client := newCalendarClient(t, provider.URL)
	input := calendar.CreateEventInput{
		CalendarID: "primary", Summary: "Offsite",
		Start: calendar.EventDateTime{Date: "2026-03-07"}, End: calendar.EventDateTime{Date: "2026-03-09"},
	}
	result, err := sdkgo.RunMutation(newCalendarDexContext("create-all-day"), client.CreateEvent(), calendarConnection, input)
	require.NoError(t, err)
	require.Equal(t, calendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.Event.IsAllDay)
	body := decodeRequestBody(t, provider.request(t, 1))
	require.Equal(t, map[string]any{"date": "2026-03-07"}, body["start"])
	require.Equal(t, map[string]any{"date": "2026-03-09"}, body["end"])
	require.NotContains(t, body, "attendees")
	require.NotContains(t, body, "conferenceData")
	require.Empty(t, provider.request(t, 1).query.Get("conferenceDataVersion"))
}

func TestCreateEventRejectsInvalidInputBeforeCallingGoogle(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	for _, test := range []struct {
		name    string
		change  func(*calendar.CreateEventInput)
		message string
	}{
		{name: "naive start", change: func(input *calendar.CreateEventInput) { input.Start.DateTime = "2026-02-24T09:00:00" }, message: "explicit offset"},
		{name: "missing zone", change: func(input *calendar.CreateEventInput) { input.End.TimeZone = "" }, message: "end.timeZone is required"},
		{name: "reversed range", change: func(input *calendar.CreateEventInput) { input.End.DateTime = "2026-02-24T08:00:00-08:00" }, message: "must be after start"},
		{name: "blank summary", change: func(input *calendar.CreateEventInput) { input.Summary = " " }, message: "summary is required"},
		{name: "display name attendee", change: func(input *calendar.CreateEventInput) {
			input.Attendees = []calendar.EventAttendeeInput{{Email: "Priya <priya@meridian.example.com>"}}
		}, message: "bare address"},
		{name: "duplicate attendee", change: func(input *calendar.CreateEventInput) {
			input.Attendees = []calendar.EventAttendeeInput{{Email: "a@example.com"}, {Email: "A@example.com"}}
		}, message: "duplicated"},
		{name: "invalid client event ID", change: func(input *calendar.CreateEventInput) { input.EventID = "Kickoff-123" }, message: "base32hex"},
		{name: "short client event ID", change: func(input *calendar.CreateEventInput) { input.EventID = "abc" }, message: "base32hex"},
		{name: "unknown notification mode", change: func(input *calendar.CreateEventInput) { input.SendUpdates = "everyone" }, message: "sendUpdates"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := timedMeetingInput()
			test.change(&input)
			result, err := sdkgo.RunMutation(newCalendarDexContext("create-invalid"), client.CreateEvent(), calendarConnection, input)
			require.NoError(t, err)
			require.Equal(t, calendar.CreateEventBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestCreateEventClassifiesConclusiveRejectionsAndInvalidResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "read-only calendar", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"requiredAccessLevel"}]}}`, branch: calendar.CreateEventBranchProviderRejected},
		{name: "invalid attendee", status: http.StatusBadRequest, body: `{"error":{"errors":[{"reason":"invalidAttendeeEmail"}]}}`, branch: calendar.CreateEventBranchProviderRejected},
		{name: "another event ID", status: http.StatusOK, body: timedEventJSON("someotherid", "Kickoff"), branch: calendar.CreateEventBranchInvalidResponse},
		{name: "not JSON", status: http.StatusOK, body: `<html>`, branch: calendar.CreateEventBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusNotFound, `{}`)
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newCalendarDexContext("create-"+test.name), client.CreateEvent(), calendarConnection, timedMeetingInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}

// eventStore is a minimal stateful Google Calendar event collection for the primary calendar.
type eventStore struct {
	mutex                    sync.Mutex
	events                   map[string]map[string]any
	inserts                  int
	hidesEventsFromFirstRead bool
	afterInsert              func(http.ResponseWriter)
}

func newEventStore() *eventStore {
	return &eventStore{events: map[string]map[string]any{}}
}

func (store *eventStore) put(eventID string, status string) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.events[eventID] = map[string]any{
		"id": eventID, "status": status, "summary": "Existing", "etag": `"1"`,
		"start": map[string]any{"dateTime": "2026-02-24T09:00:00-08:00", "timeZone": "America/Los_Angeles"},
		"end":   map[string]any{"dateTime": "2026-02-24T10:00:00-08:00", "timeZone": "America/Los_Angeles"},
	}
}

func (store *eventStore) insertCount() int {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	return store.inserts
}

func (store *eventStore) serve(t *testing.T) func(http.ResponseWriter, *http.Request, int) {
	return func(response http.ResponseWriter, request *http.Request, index int) {
		store.mutex.Lock()
		defer store.mutex.Unlock()
		switch {
		case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/calendars/primary/events/"):
			event, ok := store.events[strings.TrimPrefix(request.URL.Path, "/calendars/primary/events/")]
			if !ok || (store.hidesEventsFromFirstRead && index == 0) {
				writeJSON(t, response, http.StatusNotFound, `{"error":{"errors":[{"reason":"notFound"}]}}`)
				return
			}
			writeEventJSON(t, response, event)
		case request.Method == http.MethodPost && request.URL.Path == "/calendars/primary/events":
			var event map[string]any
			require.NoError(t, json.NewDecoder(request.Body).Decode(&event))
			eventID, _ := event["id"].(string)
			if _, exists := store.events[eventID]; exists {
				writeJSON(t, response, http.StatusConflict, `{"error":{"errors":[{"reason":"duplicate"}]}}`)
				return
			}
			event["status"] = "confirmed"
			event["etag"] = `"1"`
			store.events[eventID] = event
			store.inserts++
			if store.afterInsert != nil {
				store.afterInsert(response)
				return
			}
			writeEventJSON(t, response, event)
		default:
			http.NotFound(response, request)
		}
	}
}

func writeEventJSON(t *testing.T, response http.ResponseWriter, event map[string]any) {
	t.Helper()
	contents, err := json.Marshal(event)
	require.NoError(t, err)
	writeJSON(t, response, http.StatusOK, string(contents))
}
