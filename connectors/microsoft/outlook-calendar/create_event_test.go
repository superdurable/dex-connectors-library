// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const markerPropertyID = "String {e8548d82-fea6-423a-bad8-7b6da468e968} Name DexIdempotencyKey"

func meetingCreateInput() outlookcalendar.CreateEventInput {
	return outlookcalendar.CreateEventInput{
		CalendarID: "team-calendar", Subject: "Kickoff", Body: "Agenda to follow.", Location: "Room 4",
		Start: losAngeles("2026-02-24T09:00:00-08:00"), End: losAngeles("2026-02-24T10:00:00-08:00"),
		Attendees:            []outlookcalendar.EventAttendeeInput{{Email: "priya@contoso.com"}, {Email: "sam@contoso.com", IsOptional: true}},
		RequestOnlineMeeting: true,
	}
}

func TestCreateEventLooksTheKeyUpThenPostsWithTransactionIDAndMarker(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		transactionID := decodeRequestBody(t, recordedRequest{body: mustReadBody(t, request)})["transactionId"].(string)
		writeJSON(t, response, http.StatusCreated, strings.Replace(timedEventJSON("created-event", "Kickoff"), `"id"`, `"transactionId":"`+transactionID+`","id"`, 1))
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newCalendarDexContext("create"), client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch, "%+v", result.Failure)
	require.False(t, result.Value.WasAlreadyCreated)
	require.Equal(t, "created-event", result.Value.Event.ID)
	require.Equal(t, "team-calendar", result.Value.Event.CalendarID)
	require.Equal(t, losAngeles("2026-02-24T09:00:00-08:00"), result.Value.Event.Start, "times return in the event's zone")
	require.Equal(t, []string{http.MethodGet, http.MethodPost}, provider.methods())

	key := string(result.Receipt.IdempotencyKey)
	require.Equal(t, string(result.Receipt.CallID), key, "a blank transactionId uses the Call ID")
	lookup := provider.request(t, 0)
	require.Equal(t, "/v1.0/me/events", lookup.path)
	require.Equal(t, "singleValueExtendedProperties/Any(ep: ep/id eq '"+markerPropertyID+"' and ep/value eq '"+key+"')", lookup.query.Get("$filter"))

	create := provider.request(t, 1)
	require.Equal(t, "/v1.0/me/calendars/team-calendar/events", create.path)
	payload := decodeRequestBody(t, create)
	require.Equal(t, key, payload["transactionId"])
	require.Equal(t, []any{map[string]any{"id": markerPropertyID, "value": key}}, payload["singleValueExtendedProperties"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-24T09:00:00", "timeZone": "America/Los_Angeles"}, payload["start"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-24T10:00:00", "timeZone": "America/Los_Angeles"}, payload["end"])
	require.Equal(t, map[string]any{"contentType": "text", "content": "Agenda to follow."}, payload["body"])
	require.Equal(t, map[string]any{"displayName": "Room 4"}, payload["location"])
	require.Equal(t, []any{
		map[string]any{"emailAddress": map[string]any{"address": "priya@contoso.com"}, "type": "required"},
		map[string]any{"emailAddress": map[string]any{"address": "sam@contoso.com"}, "type": "optional"},
	}, payload["attendees"])
	require.Equal(t, true, payload["isOnlineMeeting"])
	require.Equal(t, "teamsForBusiness", payload["onlineMeetingProvider"])
	require.NotContains(t, payload, "isAllDay")
}

func TestCreateEventEncodesTheLookupFilterWithPercentTwenty(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.NotContains(t, request.URL.RawQuery, "+", "Graph's OData parser needs %20 for spaces")
			require.Contains(t, request.URL.RawQuery, "%20and%20ep%2Fvalue%20eq%20")
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, timedEventJSON("created-event", "Kickoff"))
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCalendarDexContext("create-encoding"), client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
}

func TestCreateEventReturnsTheEventAnEarlierAttemptCreatedWithoutPosting(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("first-attempt-event", "Kickoff")+`]}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunMutation(newCalendarDexContext("create-retry"), client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.WasAlreadyCreated)
	require.Equal(t, "first-attempt-event", result.Value.Event.ID)
	require.Empty(t, result.Value.DuplicateEventIDs)
	require.Equal(t, []string{http.MethodGet}, provider.methods())
}

func TestCreateEventReportsDuplicatesTheTransactionIDGuardMissed(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("first", "Kickoff")+`,`+timedEventJSON("second", "Kickoff")+`]}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCalendarDexContext("create-duplicates"), client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.Equal(t, "first", result.Value.Event.ID)
	require.Equal(t, []string{"second"}, result.Value.DuplicateEventIDs)
}

func TestCreateEventRetriesAnUnknownOutcomeAndTheRetryFindsTheEvent(t *testing.T) {
	stored := false
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch {
		case request.Method == http.MethodGet && stored:
			writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("stored-event", "Kickoff")+`]}`)
		case request.Method == http.MethodGet:
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
		default:
			// Graph stored the event, but the response is lost behind a gateway error.
			stored = true
			writeJSON(t, response, http.StatusServiceUnavailable, `{"error":{"code":"serviceNotAvailable"}}`)
		}
	})
	client := newCalendarClient(t, provider.URL)
	context := newCalendarDexContext("create-unknown")

	_, err := sdkgo.RunMutation(context, client.CreateEvent(), calendarConnection, meetingCreateInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an unknown create outcome retries instead of failing or creating twice")

	result, err := sdkgo.RunMutation(context, client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.WasAlreadyCreated)
	require.Equal(t, []string{http.MethodGet, http.MethodPost, http.MethodGet}, provider.methods(), "the retry sent no second POST")
	require.Equal(t, provider.request(t, 0).query.Get("$filter"), provider.request(t, 2).query.Get("$filter"), "both attempts used one key")
}

func TestCreateEventReadsTheKeyBackWhenGraphRejectsARepeatedTransactionID(t *testing.T) {
	for _, status := range []int{http.StatusConflict, http.StatusBadRequest} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, index int) {
				switch index {
				case 0:
					writeJSON(t, response, http.StatusOK, `{"value":[]}`)
				case 1:
					writeJSON(t, response, status, `{"error":{"code":"ErrorDuplicateTransactionId"}}`)
				default:
					writeJSON(t, response, http.StatusOK, `{"value":[`+timedEventJSON("concurrent-event", "Kickoff")+`]}`)
				}
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newCalendarDexContext("create-repeat"), client.CreateEvent(), calendarConnection, meetingCreateInput())
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
			require.True(t, result.Value.WasAlreadyCreated)
			require.Equal(t, "concurrent-event", result.Value.Event.ID)
		})
	}
}

func TestCreateEventRetriesAConflictWithoutTheEventAndRejectsABadRequest(t *testing.T) {
	for _, test := range []struct {
		status    int
		isRetried bool
	}{{status: http.StatusConflict, isRetried: true}, {status: http.StatusBadRequest}} {
		t.Run(http.StatusText(test.status), func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodGet {
					writeJSON(t, response, http.StatusOK, `{"value":[]}`)
					return
				}
				writeJSON(t, response, test.status, `{"error":{"code":"ErrorInvalidRequest","message":"SENTINEL"}}`)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newCalendarDexContext("create-rejected"), client.CreateEvent(), calendarConnection, meetingCreateInput())
			if test.isRetried {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "a concurrent attempt may still be committing the event")
				return
			}
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.CreateEventBranchProviderRejected, result.Branch)
			require.Equal(t, "provider rejected the request with HTTP 400 (ErrorInvalidRequest)", result.Failure.Message)
		})
	}
}

func TestCreateEventRejectsAnEventWithAnotherTransactionID(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, strings.Replace(timedEventJSON("other", "Kickoff"), `"id"`, `"transactionId":"someone-else","id"`, 1))
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newCalendarDexContext("create-other"), client.CreateEvent(), calendarConnection, meetingCreateInput())
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchInvalidResponse, result.Branch)
}

func TestCreateEventUsesACallerTransactionIDAndAnAllDayMidnight(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"value":[]}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"id":"offsite","isAllDay":true,"showAs":"oof",`+
			`"start":{"dateTime":"2026-02-24T08:00:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-02-25T08:00:00.0000000","timeZone":"UTC"}}`)
	})
	client := newCalendarClient(t, provider.URL)
	input := outlookcalendar.CreateEventInput{
		TransactionID: "offsite-2026-02-24", Subject: "Offsite", IsAllDay: true,
		Start: losAngeles("2026-02-24T00:00:00-08:00"), End: losAngeles("2026-02-25T00:00:00-08:00"),
	}
	result, err := sdkgo.RunMutation(newCalendarDexContext("create-all-day"), client.CreateEvent(), calendarConnection, input)
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.CreateEventBranchCreated, result.Branch)
	require.True(t, result.Value.Event.IsAllDay)
	require.Equal(t, losAngeles("2026-02-24T00:00:00-08:00"), result.Value.Event.Start)
	require.Equal(t, sdkgo.IdempotencyKey("offsite-2026-02-24"), result.Receipt.IdempotencyKey)
	payload := decodeRequestBody(t, provider.request(t, 1))
	require.Equal(t, "/v1.0/me/calendar/events", provider.request(t, 1).path, "a blank calendar ID uses the default calendar")
	require.Equal(t, "offsite-2026-02-24", payload["transactionId"])
	require.Equal(t, true, payload["isAllDay"])
	require.Equal(t, map[string]any{"dateTime": "2026-02-24T00:00:00", "timeZone": "America/Los_Angeles"}, payload["start"])
}

func TestCreateEventRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	for _, test := range []struct {
		name    string
		change  func(*outlookcalendar.CreateEventInput)
		message string
	}{
		{name: "naive start", change: func(input *outlookcalendar.CreateEventInput) { input.Start.DateTime = "2026-02-24T09:00:00" }, message: "explicit offset"},
		{name: "missing zone", change: func(input *outlookcalendar.CreateEventInput) { input.End.TimeZone = "" }, message: "end.timeZone is required"},
		{name: "blank subject", change: func(input *outlookcalendar.CreateEventInput) { input.Subject = " " }, message: "subject is required"},
		{name: "duplicate attendee", change: func(input *outlookcalendar.CreateEventInput) {
			input.Attendees = append(input.Attendees, outlookcalendar.EventAttendeeInput{Email: "PRIYA@contoso.com"})
		}, message: "duplicated"},
		{name: "display-name attendee", change: func(input *outlookcalendar.CreateEventInput) {
			input.Attendees = []outlookcalendar.EventAttendeeInput{{Email: "Priya <priya@contoso.com>"}}
		}, message: "bare address"},
		{name: "unsafe transaction ID", change: func(input *outlookcalendar.CreateEventInput) { input.TransactionID = "x' or 1 eq 1" }, message: "transactionId"},
		{name: "padded calendar ID", change: func(input *outlookcalendar.CreateEventInput) { input.CalendarID = " team " }, message: "whitespace"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := meetingCreateInput()
			test.change(&input)
			result, err := sdkgo.RunMutation(newCalendarDexContext("create-invalid-"+test.name), client.CreateEvent(), calendarConnection, input)
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.CreateEventBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}
