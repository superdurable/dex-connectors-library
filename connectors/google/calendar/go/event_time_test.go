// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
)

func TestValidateEventTimesAcceptsExplicitTimedAndAllDayRanges(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end calendar.EventDateTime
	}{
		{
			name:  "timed with offset and matching zone",
			start: calendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "America/Los_Angeles"},
			end:   calendar.EventDateTime{DateTime: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles"},
		},
		{
			name:  "timed in UTC displayed in another zone",
			start: calendar.EventDateTime{DateTime: "2026-02-24T17:00:00Z", TimeZone: "America/Los_Angeles"},
			end:   calendar.EventDateTime{DateTime: "2026-02-24T18:00:00Z", TimeZone: "America/Los_Angeles"},
		},
		{
			name:  "timed across zones whose wall clocks look reversed",
			start: calendar.EventDateTime{DateTime: "2026-02-24T12:00:00-05:00", TimeZone: "America/New_York"},
			end:   calendar.EventDateTime{DateTime: "2026-02-24T10:00:00-08:00", TimeZone: "America/Los_Angeles"},
		},
		{
			name:  "timed with fractional seconds",
			start: calendar.EventDateTime{DateTime: "2026-02-24T09:00:00.5+05:30", TimeZone: "Asia/Kolkata"},
			end:   calendar.EventDateTime{DateTime: "2026-02-24T09:30:00+05:30", TimeZone: "Asia/Kolkata"},
		},
		{
			name:  "one all-day date with exclusive end",
			start: calendar.EventDateTime{Date: "2026-02-24"},
			end:   calendar.EventDateTime{Date: "2026-02-25"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, calendar.ValidateEventTimes(test.start, test.end))
		})
	}
}

func TestValidateEventTimesRejectsAmbiguousTimes(t *testing.T) {
	losAngeles := func(dateTime string) calendar.EventDateTime {
		return calendar.EventDateTime{DateTime: dateTime, TimeZone: "America/Los_Angeles"}
	}
	for _, test := range []struct {
		name       string
		start, end calendar.EventDateTime
		message    string
	}{
		{
			name: "naive start without an offset", start: losAngeles("2026-02-20T14:00:00"), end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: `start.dateTime "2026-02-20T14:00:00" must be RFC 3339 with an explicit offset`,
		},
		{
			name: "timed without a time zone", start: calendar.EventDateTime{DateTime: "2026-02-20T14:00:00-08:00"}, end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: "start.timeZone is required for a timed event",
		},
		{
			name: "abbreviation instead of an IANA name", start: calendar.EventDateTime{DateTime: "2026-02-20T14:00:00-08:00", TimeZone: "Pacific Standard Time"}, end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: "is not a known IANA time zone name",
		},
		{
			name: "process-local zone", start: calendar.EventDateTime{DateTime: "2026-02-20T14:00:00-08:00", TimeZone: "Local"}, end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: "must be an IANA time zone name",
		},
		{
			name: "end equal to start in another offset", start: losAngeles("2026-02-20T14:00:00-08:00"), end: calendar.EventDateTime{DateTime: "2026-02-20T22:00:00Z", TimeZone: "UTC"},
			message: "end 2026-02-20T22:00:00Z must be after start 2026-02-20T14:00:00-08:00 (2026-02-20T22:00:00Z and 2026-02-20T22:00:00Z in UTC)",
		},
		{
			name: "later wall clock but earlier instant", start: losAngeles("2026-02-20T10:00:00-08:00"), end: calendar.EventDateTime{DateTime: "2026-02-20T12:00:00-05:00", TimeZone: "America/New_York"},
			message: "(2026-02-20T17:00:00Z and 2026-02-20T18:00:00Z in UTC)",
		},
		{
			name: "both date and dateTime", start: calendar.EventDateTime{Date: "2026-02-20", DateTime: "2026-02-20T14:00:00-08:00"}, end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: "start sets both date and dateTime",
		},
		{
			name: "neither date nor dateTime", start: calendar.EventDateTime{TimeZone: "America/Los_Angeles"}, end: losAngeles("2026-02-20T15:00:00-08:00"),
			message: "start must set date for an all-day event or dateTime for a timed event",
		},
		{
			name: "all-day with a time zone", start: calendar.EventDateTime{Date: "2026-02-20", TimeZone: "America/Los_Angeles"}, end: calendar.EventDateTime{Date: "2026-02-21"},
			message: "start is an all-day date, which has no time zone",
		},
		{
			name: "all-day date with a time", start: calendar.EventDateTime{Date: "2026-02-20T00:00:00Z"}, end: calendar.EventDateTime{Date: "2026-02-21"},
			message: `start.date "2026-02-20T00:00:00Z" must use YYYY-MM-DD`,
		},
		{
			name: "all-day end equal to start", start: calendar.EventDateTime{Date: "2026-02-20"}, end: calendar.EventDateTime{Date: "2026-02-20"},
			message: "all-day end date 2026-02-20 is exclusive and must follow start date 2026-02-20",
		},
		{
			name: "mixed all-day start and timed end", start: calendar.EventDateTime{Date: "2026-02-20"}, end: losAngeles("2026-02-21T00:00:00-08:00"),
			message: "start and end must both be all-day dates or both be timed dateTime values",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := calendar.ValidateEventTimes(test.start, test.end)
			require.Error(t, err)
			require.Contains(t, err.Error(), test.message)
		})
	}
}

func TestEventDateTimeInstantUsesOffsetsAndNeedsAZoneForAllDayDates(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// The offset fixes the instant; the display zone and the all-day location do not move it.
	timed := calendar.EventDateTime{DateTime: "2026-02-20T14:00:00-08:00", TimeZone: "America/New_York"}
	instant, err := timed.Instant(newYork)
	require.NoError(t, err)
	require.Equal(t, "2026-02-20T22:00:00Z", instant.UTC().Format(time.RFC3339))
	require.False(t, timed.IsAllDay())

	allDay := calendar.EventDateTime{Date: "2026-03-08"}
	require.True(t, allDay.IsAllDay())
	_, err = allDay.Instant(nil)
	require.ErrorContains(t, err, "needs a calendar time zone")
	midnight, err := allDay.Instant(newYork)
	require.NoError(t, err)
	require.Equal(t, "2026-03-08T05:00:00Z", midnight.UTC().Format(time.RFC3339))
	// Daylight saving time starts that morning, so the next midnight is only 23 hours later.
	nextMidnight, err := calendar.EventDateTime{Date: "2026-03-09"}.Instant(newYork)
	require.NoError(t, err)
	require.Equal(t, 23*time.Hour, nextMidnight.Sub(midnight))

	_, err = calendar.EventDateTime{DateTime: "2026-02-20T14:00:00"}.Instant(newYork)
	require.ErrorContains(t, err, "explicit offset")
}
