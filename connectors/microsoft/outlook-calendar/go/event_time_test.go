// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
)

func losAngeles(dateTime string) outlookcalendar.EventDateTime {
	return outlookcalendar.EventDateTime{DateTime: dateTime, TimeZone: "America/Los_Angeles"}
}

func TestValidateEventTimesRejectsAmbiguousOrInvertedTimes(t *testing.T) {
	for _, test := range []struct {
		name       string
		start, end outlookcalendar.EventDateTime
		isAllDay   bool
		message    string
	}{
		{name: "naive start", start: losAngeles("2026-02-24T09:00:00"), end: losAngeles("2026-02-24T10:00:00-08:00"), message: "explicit offset"},
		{name: "missing zone", start: outlookcalendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00"}, end: losAngeles("2026-02-24T10:00:00-08:00"), message: "start.timeZone is required"},
		{name: "Windows zone name", start: outlookcalendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "Pacific Standard Time"}, end: losAngeles("2026-02-24T10:00:00-08:00"), message: "not a known IANA"},
		{name: "Local zone", start: outlookcalendar.EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "Local"}, end: losAngeles("2026-02-24T10:00:00-08:00"), message: "IANA time zone name"},
		// 09:00 in Los Angeles is 17:00Z, which is after 16:30Z even though the wall clock reads earlier.
		{name: "end before start across offsets", start: losAngeles("2026-02-24T09:00:00-08:00"), end: outlookcalendar.EventDateTime{DateTime: "2026-02-24T11:30:00-05:00", TimeZone: "America/New_York"}, message: "must be after start"},
		{name: "all-day not at midnight", start: losAngeles("2026-02-24T09:00:00-08:00"), end: losAngeles("2026-02-25T00:00:00-08:00"), isAllDay: true, message: "must be midnight"},
		{name: "all-day in two zones", start: losAngeles("2026-02-24T00:00:00-08:00"), end: outlookcalendar.EventDateTime{DateTime: "2026-02-25T00:00:00-05:00", TimeZone: "America/New_York"}, isAllDay: true, message: "same timeZone"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.ErrorContains(t, outlookcalendar.ValidateEventTimes(test.start, test.end, test.isAllDay), test.message)
		})
	}
	require.NoError(t, outlookcalendar.ValidateEventTimes(losAngeles("2026-02-24T09:00:00-08:00"), outlookcalendar.EventDateTime{
		DateTime: "2026-02-24T13:00:00-05:00", TimeZone: "America/New_York",
	}, false), "an end in another zone is compared by instant")
	require.NoError(t, outlookcalendar.ValidateEventTimes(losAngeles("2026-02-24T00:00:00-08:00"), losAngeles("2026-02-25T00:00:00-08:00"), true))
}

func TestEventDateTimeInstantUsesItsOwnOffset(t *testing.T) {
	instant, err := losAngeles("2026-02-24T09:00:00-08:00").Instant()
	require.NoError(t, err)
	require.True(t, instant.Equal(time.Date(2026, time.February, 24, 17, 0, 0, 0, time.UTC)))
	_, err = losAngeles("2026-02-24T09:00:00").Instant()
	require.ErrorContains(t, err, "explicit offset")
}
