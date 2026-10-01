// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

const (
	calendarDateLayout = "2006-01-02"
	// modifiedSinceLayout is the UTC timestamp format Xero documents for If-Modified-Since.
	modifiedSinceLayout = "2006-01-02T15:04:05"
)

// dotNetDatePattern matches Xero's Microsoft .NET JSON date, such as /Date(1518685950940+0000)/.
var dotNetDatePattern = regexp.MustCompile(`^/Date\((-?[0-9]{1,15})([+-][0-9]{4})?\)/$`)

var errInvalidXeroDate = errors.New("value is not a Xero date")

// validateCalendarDate checks a YYYY-MM-DD input date; the error names the field, never the value.
func validateCalendarDate(fieldName string, value string) error {
	if _, err := time.Parse(calendarDateLayout, value); err != nil {
		return fmt.Errorf("%s must be a calendar date written YYYY-MM-DD, such as 2026-10-01", fieldName)
	}
	return nil
}

// calendarDateFromWire prefers DateString over the .NET date; blank values return "" for omitted dates.
func calendarDateFromWire(dateString string, dotNetDate string) (string, error) {
	if len(dateString) >= len(calendarDateLayout) {
		if _, err := time.Parse(calendarDateLayout, dateString[:len(calendarDateLayout)]); err == nil {
			return dateString[:len(calendarDateLayout)], nil
		}
		return "", errInvalidXeroDate
	}
	if dateString != "" {
		return "", errInvalidXeroDate
	}
	if dotNetDate == "" {
		return "", nil
	}
	instant, err := parseDotNetDate(dotNetDate)
	if err != nil {
		return "", err
	}
	return instant.Format(calendarDateLayout), nil
}

// timestampFromWire parses Xero's UpdatedDateUTC .NET date; blank returns the zero time.
func timestampFromWire(dotNetDate string) (time.Time, error) {
	if dotNetDate == "" {
		return time.Time{}, nil
	}
	return parseDotNetDate(dotNetDate)
}

// parseDotNetDate reads milliseconds since the Unix epoch; the optional offset does not change the instant.
func parseDotNetDate(value string) (time.Time, error) {
	match := dotNetDatePattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, errInvalidXeroDate
	}
	milliseconds, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return time.Time{}, errInvalidXeroDate
	}
	return time.UnixMilli(milliseconds).UTC(), nil
}
