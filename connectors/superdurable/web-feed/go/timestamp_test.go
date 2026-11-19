// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseLooseTimestampReadsCommonSpellings(t *testing.T) {
	for value, expected := range map[string]string{
		"2026-11-10T09:30:00Z":                 "2026-11-10T09:30:00Z",
		"2026-11-10t09:30:00z":                 "2026-11-10T09:30:00Z",
		"2026-11-10T09:30:00.123456+01:00":     "2026-11-10T08:30:00.123456Z",
		"2026-11-10T09:30:00+0100":             "2026-11-10T08:30:00Z",
		"2026-11-10T09:30+01:00":               "2026-11-10T08:30:00Z",
		"2026-11-10T09:30:00":                  "2026-11-10T09:30:00Z",
		"2026-11-10 09:30:00":                  "2026-11-10T09:30:00Z",
		"2026-11-10 09:30:00 +0000 UTC":        "2026-11-10T09:30:00Z",
		"2026-11-10 09:30:00 -0800 PST":        "2026-11-10T17:30:00Z",
		"2026-11-10 09:30:00 UTC":              "2026-11-10T09:30:00Z",
		"2026-11-10":                           "2026-11-10T00:00:00Z",
		"2026-11":                              "2026-11-01T00:00:00Z",
		"2026":                                 "2026-01-01T00:00:00Z",
		"Tue, 10 Nov 2026 09:30:00 GMT":        "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 09:30:00 +0000":      "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 09:30:00 +01:00":     "2026-11-10T08:30:00Z",
		"Tue, 10 Nov 2026 09:30:00 UT":         "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 04:30:00 EST":        "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 02:30:00 PDT":        "2026-11-10T09:30:00Z",
		"Tue,10 Nov 2026 09:30:00 GMT":         "2026-11-10T09:30:00Z",
		"Tuesday, 10 November 2026 09:30:00 Z": "2026-11-10T09:30:00Z",
		"10 Sept 2026 09:30 GMT":               "2026-09-10T09:30:00Z",
		"10 nov 2026 09:30:00 gmt":             "2026-11-10T09:30:00Z",
		"10 Nov 26 09:30:00 GMT":               "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 09:30:00":            "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026":                     "2026-11-10T00:00:00Z",
		"Tuesday, 10-Nov-26 09:30:00 GMT":      "2026-11-10T09:30:00Z",
		"Nov 10, 2026 09:30:00 GMT":            "2026-11-10T09:30:00Z",
		"Tue Nov 10 09:30:00 2026":             "2026-11-10T09:30:00Z",
		"Tue Nov 10 09:30:00 +0000 2026":       "2026-11-10T09:30:00Z",
		"Tue, 10 Nov 2026 09:30:00 GMT+0100":   "2026-11-10T08:30:00Z",
		"  Tue,   10 Nov 2026\n09:30:00 GMT  ": "2026-11-10T09:30:00Z",
	} {
		t.Run(value, func(t *testing.T) {
			parsed, isParsed := parseLooseTimestamp(value)
			require.True(t, isParsed)
			require.Equal(t, expected, parsed.Format(time.RFC3339Nano))
			require.Equal(t, time.UTC, parsed.Location())
		})
	}
}

func TestParseLooseTimestampRejectsMissingDates(t *testing.T) {
	for _, value := range []string{
		"", "yesterday", "0001-01-01T00:00:00Z", "1970-01-01T00:00:00Z", "Thu, 01 Jan 1970 00:00:00 GMT",
		"2026-13-01", "32 Nov 2026 09:30:00 GMT", "2026-11-10T09:30:00Z and more text that is far too long to be a date",
	} {
		_, isParsed := parseLooseTimestamp(value)
		require.False(t, isParsed, value)
	}
	require.Nil(t, optionalTimestamp("not a date"))
}
