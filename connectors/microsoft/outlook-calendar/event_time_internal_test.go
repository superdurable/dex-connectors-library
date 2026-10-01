// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEncodeGraphBoundaryWritesTheWallTimeInTheEventZone(t *testing.T) {
	require.Equal(t, graphDateTimeTimeZone{DateTime: "2026-02-24T09:00:00", TimeZone: "America/Los_Angeles"},
		encodeGraphBoundary(EventDateTime{DateTime: "2026-02-24T17:00:00Z", TimeZone: "America/Los_Angeles"}),
		"the instant's own offset need not match the zone; the wall time is computed from the instant")
	require.Equal(t, graphDateTimeTimeZone{DateTime: "2026-02-25T02:00:00", TimeZone: "Asia/Tokyo"},
		encodeGraphBoundary(EventDateTime{DateTime: "2026-02-24T09:00:00-08:00", TimeZone: "Asia/Tokyo"}))
}

func TestEncodeGraphBoundaryFallsBackToUTCForAnAmbiguousWallTime(t *testing.T) {
	// 1 November 2026 01:30 happens twice in Los Angeles: at 08:30Z (PDT) and at 09:30Z (PST).
	for _, instant := range []string{"2026-11-01T01:30:00-07:00", "2026-11-01T01:30:00-08:00"} {
		boundary := EventDateTime{DateTime: instant, TimeZone: "America/Los_Angeles"}
		encoded := encodeGraphBoundary(boundary)
		require.Equal(t, graphUTCTimeZone, encoded.TimeZone, instant)
		parsed, err := time.ParseInLocation(graphWallTimeLayout, encoded.DateTime, time.UTC)
		require.NoError(t, err)
		expected, err := boundary.Instant()
		require.NoError(t, err)
		require.True(t, parsed.Equal(expected), "the instant never moves")
	}
	require.Equal(t, "America/Los_Angeles", encodeGraphBoundary(EventDateTime{DateTime: "2026-11-01T03:30:00-08:00", TimeZone: "America/Los_Angeles"}).TimeZone,
		"an unambiguous time on the same day keeps its zone")
	require.Equal(t, "America/Los_Angeles", encodeGraphBoundary(EventDateTime{DateTime: "2026-03-08T03:30:00-07:00", TimeZone: "America/Los_Angeles"}).TimeZone,
		"the spring-forward gap leaves no ambiguous wall time after it")
}

func TestDecodeGraphBoundaryRendersUTCInTheDisplayZone(t *testing.T) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	boundary, instant, err := decodeGraphBoundary("start", &graphDateTimeTimeZone{DateTime: "2026-02-24T17:00:00.0000000", TimeZone: "UTC"}, newYork, "America/New_York")
	require.NoError(t, err)
	require.Equal(t, EventDateTime{DateTime: "2026-02-24T12:00:00-05:00", TimeZone: "America/New_York"}, boundary)
	require.True(t, instant.Equal(time.Date(2026, time.February, 24, 17, 0, 0, 0, time.UTC)))

	_, _, err = decodeGraphBoundary("start", &graphDateTimeTimeZone{DateTime: "2026-02-24T09:00:00.0000000", TimeZone: "Pacific Standard Time"}, time.UTC, "UTC")
	require.ErrorContains(t, err, "neither UTC nor an IANA name", "a Windows zone means Graph ignored the UTC preference")
	_, _, err = decodeGraphBoundary("start", nil, time.UTC, "UTC")
	require.ErrorContains(t, err, "missing")
	_, _, err = decodeGraphBoundary("start", &graphDateTimeTimeZone{DateTime: "24 Feb 2026", TimeZone: "UTC"}, time.UTC, "UTC")
	require.Error(t, err)
}

func TestHasDelegatedCalendarScopeAcceptsShortAndURIForms(t *testing.T) {
	for _, scope := range []string{"", "Calendars.ReadWrite", "openid profile calendars.readwrite", "https://graph.microsoft.com/Calendars.ReadWrite https://graph.microsoft.com/User.Read"} {
		require.True(t, hasDelegatedCalendarScope(scope), scope)
	}
	for _, scope := range []string{"Calendars.Read", "offline_access", "https://graph.microsoft.com/Calendars.ReadWrite.Shared"} {
		require.False(t, hasDelegatedCalendarScope(scope), scope)
	}
}
