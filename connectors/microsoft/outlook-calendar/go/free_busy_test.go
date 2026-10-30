// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func freeBusyInput(schedules ...string) outlookcalendar.QueryFreeBusyInput {
	return outlookcalendar.QueryFreeBusyInput{
		Schedules: schedules, TimeMin: "2026-02-24T09:00:00-08:00", TimeMax: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles",
	}
}

func scheduleJSON(scheduleID string, availabilityView string, items string, extra string) string {
	return `{"scheduleId":"` + scheduleID + `","availabilityView":"` + availabilityView + `","scheduleItems":[` + items + `]` + extra + `}`
}

const busyItemJSON = `{"status":"busy","start":{"dateTime":"2026-02-24T17:30:00.0000000","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T18:00:00.0000000","timeZone":"UTC"}}`

func TestQueryFreeBusyAsksGraphGetScheduleForTheExactWindowInUTC(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[`+
			scheduleJSON("PRIYA@contoso.com", "02", busyItemJSON, "")+`,`+
			scheduleJSON("sam@contoso.com", "00", `{"status":"workingElsewhere","start":{"dateTime":"2026-02-24T17:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T18:00:00","timeZone":"UTC"}}`, "")+
			`]}`)
	})
	client := newCalendarClient(t, provider.URL)

	result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy"), client.QueryFreeBusy(), calendarConnection, freeBusyInput("priya@contoso.com", "sam@contoso.com"))
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.QueryFreeBusyBranchQueried, result.Branch, "%+v", result.Failure)
	request := provider.request(t, 0)
	require.Equal(t, "/v1.0/me/calendar/getSchedule", request.path)
	require.Equal(t, map[string]any{
		"schedules":                []any{"priya@contoso.com", "sam@contoso.com"},
		"startTime":                map[string]any{"dateTime": "2026-02-24T17:00:00", "timeZone": "UTC"},
		"endTime":                  map[string]any{"dateTime": "2026-02-24T18:00:00", "timeZone": "UTC"},
		"availabilityViewInterval": float64(30),
	}, decodeRequestBody(t, request))

	require.Equal(t, outlookcalendar.QueryFreeBusyOutput{
		TimeMin: "2026-02-24T09:00:00-08:00", TimeMax: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles", AvailabilityViewInterval: 30,
		Schedules: []outlookcalendar.ScheduleAvailability{
			{ScheduleID: "priya@contoso.com", IsBusy: true, IsAvailabilityKnown: true, AvailabilityView: "02",
				Items: []outlookcalendar.ScheduleItem{{Start: "2026-02-24T09:30:00-08:00", End: "2026-02-24T10:00:00-08:00", Status: "busy"}}},
			{ScheduleID: "sam@contoso.com", IsAvailabilityKnown: true, AvailabilityView: "00",
				Items: []outlookcalendar.ScheduleItem{{Start: "2026-02-24T09:00:00-08:00", End: "2026-02-24T10:00:00-08:00", Status: "workingElsewhere"}}},
		},
	}, result.Value)
}

func TestQueryFreeBusyReadsBusyTimeFromTheAvailabilityViewAlone(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"value":[`+scheduleJSON("room@contoso.com", "01", "", "")+`]}`)
	})
	client := newCalendarClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-view"), client.QueryFreeBusy(), calendarConnection, freeBusyInput("room@contoso.com"))
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.QueryFreeBusyBranchQueried, result.Branch)
	require.True(t, result.Value.Schedules[0].IsBusy, "a tentative slot without an item still counts as busy")
	require.Empty(t, result.Value.Schedules[0].Items)
}

func TestQueryFreeBusyNeverReportsUnknownAvailabilityAsFree(t *testing.T) {
	for _, test := range []struct {
		name       string
		schedule   string
		reason     string
		errorCode  string
		isReported bool
	}{
		{name: "error", schedule: scheduleJSON("ghost@contoso.com", "", `{"status":"unknown"}`, `,"error":{"message":"SENTINEL not found","responseCode":"ErrorMailRecipientNotFound"}`), reason: "error", errorCode: "ErrorMailRecipientNotFound", isReported: true},
		{name: "unknown item", schedule: scheduleJSON("priya@contoso.com", "00", `{"status":"unknown","start":{"dateTime":"2026-02-24T17:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T18:00:00","timeZone":"UTC"}}`, ""), reason: "unknownStatus", isReported: true},
		{name: "unreadable view", schedule: scheduleJSON("priya@contoso.com", "0?", "", ""), reason: "unreadableAvailabilityView", isReported: true},
		{name: "empty view", schedule: scheduleJSON("priya@contoso.com", "", "", ""), reason: "unreadableAvailabilityView", isReported: true},
		{name: "missing schedule", schedule: scheduleJSON("someone-else@contoso.com", "00", "", ""), reason: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, `{"value":[`+test.schedule+`]}`)
			})
			client := newCalendarClient(t, provider.URL)
			scheduleID := "priya@contoso.com"
			if test.name == "error" {
				scheduleID = "ghost@contoso.com"
			}
			result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-"+test.name), client.QueryFreeBusy(), calendarConnection, freeBusyInput(scheduleID))
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.QueryFreeBusyBranchIncomplete, result.Branch)
			schedule := result.Value.Schedules[0]
			require.False(t, schedule.IsAvailabilityKnown)
			require.Equal(t, test.reason, schedule.UnknownReason)
			require.Equal(t, test.errorCode, schedule.ErrorResponseCode)
			require.NotContains(t, fmt.Sprint(result), "SENTINEL", "the provider's error message is never kept")
		})
	}
}

func TestQueryFreeBusyRejectsUnboundedInputWithoutARequest(t *testing.T) {
	provider := newRecordingProvider(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newCalendarClient(t, provider.URL)
	tooMany := make([]string, 21)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("person%d@contoso.com", index)
	}
	for _, test := range []struct {
		name    string
		change  func(*outlookcalendar.QueryFreeBusyInput)
		message string
	}{
		{name: "no schedules", change: func(input *outlookcalendar.QueryFreeBusyInput) { input.Schedules = nil }, message: "1 to 20"},
		{name: "too many schedules", change: func(input *outlookcalendar.QueryFreeBusyInput) { input.Schedules = tooMany }, message: "1 to 20"},
		{name: "duplicate schedule", change: func(input *outlookcalendar.QueryFreeBusyInput) {
			input.Schedules = []string{"a@contoso.com", "A@contoso.com"}
		}, message: "duplicated"},
		{name: "not an address", change: func(input *outlookcalendar.QueryFreeBusyInput) { input.Schedules = []string{"Team Room"} }, message: "bare address"},
		{name: "naive time", change: func(input *outlookcalendar.QueryFreeBusyInput) { input.TimeMin = "2026-02-24T09:00:00" }, message: "explicit offset"},
		{name: "62-day window", change: func(input *outlookcalendar.QueryFreeBusyInput) {
			input.TimeMin, input.TimeMax = "2026-01-01T00:00:00Z", "2026-03-04T00:00:00Z"
		}, message: "shorter than 62 days"},
		{name: "interval too short", change: func(input *outlookcalendar.QueryFreeBusyInput) { input.AvailabilityViewInterval = 4 }, message: "between 5 and 1440"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := freeBusyInput("priya@contoso.com")
			test.change(&input)
			result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-invalid-"+test.name), client.QueryFreeBusy(), calendarConnection, input)
			require.NoError(t, err)
			require.Equal(t, outlookcalendar.QueryFreeBusyBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.message)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestQueryFreeBusyMapsRejectionsAndUnusableResponses(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "forbidden", status: http.StatusForbidden, body: `{"error":{"code":"ErrorAccessDenied"}}`, branch: outlookcalendar.QueryFreeBusyBranchProviderRejected},
		{name: "no value array", status: http.StatusOK, body: `{}`, branch: outlookcalendar.QueryFreeBusyBranchInvalidResponse},
		{name: "unordered item", status: http.StatusOK, body: `{"value":[` + scheduleJSON("priya@contoso.com", "2", `{"status":"busy","start":{"dateTime":"2026-02-24T18:00:00","timeZone":"UTC"},"end":{"dateTime":"2026-02-24T17:00:00","timeZone":"UTC"}}`, "") + `]}`, branch: outlookcalendar.QueryFreeBusyBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newCalendarClient(t, provider.URL)
			result, err := sdkgo.RunQuery(newCalendarDexContext("free-busy-map-"+test.name), client.QueryFreeBusy(), calendarConnection, freeBusyInput("priya@contoso.com"))
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
		})
	}
}
