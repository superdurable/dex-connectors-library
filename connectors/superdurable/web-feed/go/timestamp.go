// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"strings"
	"time"
)

// zoneOffsets maps the zone abbreviations feeds use to their offsets; Go would read an unknown
// abbreviation as UTC.
var zoneOffsets = map[string]string{
	"UT": "+0000", "UTC": "+0000", "GMT": "+0000", "Z": "+0000", "Z00:00": "+0000",
	"EST": "-0500", "EDT": "-0400", "CST": "-0600", "CDT": "-0500",
	"MST": "-0700", "MDT": "-0600", "PST": "-0800", "PDT": "-0700",
	"CET": "+0100", "CEST": "+0200", "BST": "+0100", "IST": "+0530", "JST": "+0900", "AEST": "+1000",
}

var fullMonthNames = strings.NewReplacer(
	"January", "Jan", "February", "Feb", "March", "Mar", "April", "Apr", "June", "Jun", "July", "Jul",
	"August", "Aug", "September", "Sep", "Sept", "Sep", "October", "Oct", "November", "Nov", "December", "Dec",
)

// isoLayouts are RFC 3339 and W3C datetime spellings, most specific first. A value without an offset is UTC.
var isoLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999Z0700",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04Z0700",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999 Z07:00",
	"2006-01-02 15:04:05.999999999 -0700",
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02",
	"2006-01",
	"2006",
}

// mailLayouts are RFC 822, RFC 1123, and ctime spellings after the weekday is removed and the zone made numeric.
var mailLayouts = []string{
	"2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04 -0700",
	"2 Jan 06 15:04:05 -0700",
	"2 Jan 06 15:04 -0700",
	"2 Jan 2006 15:04:05",
	"2 Jan 2006 15:04",
	"2 Jan 2006",
	"Jan 2 2006 15:04:05 -0700",
	"Jan 2 2006 15:04:05",
	"Jan 2 15:04:05 2006",
	"Jan 2 15:04:05 -0700 2006",
	"Jan 2 2006",
}

// earliestTimestamp rejects zero and epoch placeholders, which generators emit for a missing date.
var earliestTimestamp = time.Date(1970, time.January, 2, 0, 0, 0, 0, time.UTC)

// parseLooseTimestamp returns the instant in UTC; no offset means UTC, and pre-1970-01-02 dates are missing.
func parseLooseTimestamp(value string) (time.Time, bool) {
	value = collapseWhitespace(value)
	if value == "" || len(value) > 64 {
		return time.Time{}, false
	}
	if parsed, isParsed := parseISOTimestamp(value); isParsed {
		return acceptTimestamp(parsed)
	}
	if parsed, isParsed := parseMailTimestamp(value); isParsed {
		return acceptTimestamp(parsed)
	}
	return time.Time{}, false
}

func parseISOTimestamp(value string) (time.Time, bool) {
	if len(value) < 4 || value[0] < '0' || value[0] > '9' {
		return time.Time{}, false
	}
	candidate := strings.ToUpper(value)
	// A trailing UTC or GMT names the zone of a value with no offset, or repeats a zero offset.
	if strings.HasSuffix(candidate, " UTC") || strings.HasSuffix(candidate, " GMT") {
		candidate = strings.TrimSpace(candidate[:len(candidate)-4])
	}
	for _, layout := range isoLayouts {
		if parsed, err := time.Parse(layout, candidate); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func parseMailTimestamp(value string) (time.Time, bool) {
	fields := strings.Fields(strings.ReplaceAll(value, ",", " "))
	if len(fields) > 0 && isWeekday(fields[0]) {
		fields = fields[1:]
	}
	// RFC 850 writes the date as 02-Nov-26 or 02-Nov-2026.
	if len(fields) > 0 && strings.Count(fields[0], "-") == 2 {
		fields = append(strings.Split(fields[0], "-"), fields[1:]...)
	}
	for index, field := range fields {
		fields[index] = fullMonthNames.Replace(strings.ToUpper(field[:1]) + strings.ToLower(field[1:]))
		if offset, isZone := zoneOffsets[strings.ToUpper(field)]; isZone {
			fields[index] = offset
		} else if offset, isNumeric := numericOffset(field); isNumeric {
			fields[index] = offset
		}
	}
	candidate := strings.Join(fields, " ")
	for _, layout := range mailLayouts {
		if parsed, err := time.Parse(layout, candidate); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// numericOffset rewrites +hh:mm, +hhmm, and GMT+hhmm offsets as +hhmm.
func numericOffset(field string) (string, bool) {
	upper := strings.ToUpper(field)
	upper = strings.TrimPrefix(strings.TrimPrefix(upper, "GMT"), "UTC")
	if len(upper) < 5 || (upper[0] != '+' && upper[0] != '-') {
		return "", false
	}
	digits := strings.ReplaceAll(upper[1:], ":", "")
	if len(digits) != 4 {
		return "", false
	}
	for index := 0; index < len(digits); index++ {
		if digits[index] < '0' || digits[index] > '9' {
			return "", false
		}
	}
	return upper[:1] + digits, true
}

func isWeekday(field string) bool {
	switch strings.ToLower(strings.TrimSuffix(field, ".")) {
	case "mon", "tue", "tues", "wed", "thu", "thur", "thurs", "fri", "sat", "sun",
		"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		return true
	}
	return false
}

func acceptTimestamp(parsed time.Time) (time.Time, bool) {
	if parsed.Before(earliestTimestamp) {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

// optionalTimestamp parses value and returns nil when it is missing or unreadable.
func optionalTimestamp(value string) *time.Time {
	parsed, isParsed := parseLooseTimestamp(value)
	if !isParsed {
		return nil
	}
	return &parsed
}
