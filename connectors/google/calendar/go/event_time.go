// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	// Embedded zone data makes IANA validation identical on hosts without a zoneinfo database.
	_ "time/tzdata"
)

const (
	allDayDateLayout = "2006-01-02"
	// maximumQueryWindow bounds the listEvents and queryFreeBusy windows.
	maximumQueryWindow = 366 * 24 * time.Hour
)

// EventDateTime is one event boundary in Google Calendar's native shape. Exactly one
// of Date and DateTime is set, so a boundary is unambiguously all-day or timed.
//
// A timed boundary sets DateTime to an RFC 3339 instant with an explicit Z or
// ±hh:mm offset, such as 2026-02-24T09:00:00-08:00, and TimeZone to an IANA name
// such as America/Los_Angeles. The offset fixes the instant; TimeZone is the zone
// Google uses to display the event. A value without an offset, such as
// 2026-02-24T09:00:00, is rejected rather than guessed.
//
// An all-day boundary sets Date to YYYY-MM-DD and leaves TimeZone blank: an
// all-day date has no instant until it is read in a calendar's time zone. An
// all-day end date is exclusive, so a one-day event on 24 February ends on
// 2026-02-25.
type EventDateTime struct {
	// Date is the YYYY-MM-DD calendar date of an all-day boundary.
	Date string `json:"date,omitempty"`
	// DateTime is the RFC 3339 instant, with an explicit offset, of a timed boundary.
	DateTime string `json:"dateTime,omitempty"`
	// TimeZone is the IANA time zone of a timed boundary. Google may also report one on output.
	TimeZone string `json:"timeZone,omitempty"`
}

// IsAllDay reports whether the boundary is a date rather than an instant.
func (boundary EventDateTime) IsAllDay() bool { return boundary.Date != "" }

// Instant returns the boundary as an absolute time. A timed boundary uses its own
// offset and ignores allDayLocation. An all-day boundary is midnight of Date in
// allDayLocation, such as the zone named by ListEventsOutput.TimeZone; a nil
// location is an error because an all-day date has no zone of its own.
func (boundary EventDateTime) Instant(allDayLocation *time.Location) (time.Time, error) {
	switch {
	case boundary.Date != "" && boundary.DateTime != "":
		return time.Time{}, errors.New("event boundary sets both date and dateTime")
	case boundary.DateTime != "":
		return parseInstant("dateTime", boundary.DateTime)
	case boundary.Date != "":
		date, err := time.Parse(allDayDateLayout, boundary.Date)
		if err != nil {
			return time.Time{}, fmt.Errorf("all-day date %q must use YYYY-MM-DD", boundary.Date)
		}
		if allDayLocation == nil {
			return time.Time{}, errors.New("an all-day date needs a calendar time zone to become an instant")
		}
		return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, allDayLocation), nil
	default:
		return time.Time{}, errors.New("event boundary sets neither date nor dateTime")
	}
}

// ValidateEventTimes reports whether start and end describe one unambiguous event.
// Both boundaries must be timed or both all-day. Timed boundaries need an RFC 3339
// offset and an IANA TimeZone, and end must be a later instant than start even when
// the two offsets differ. All-day boundaries take Date only, and the exclusive end
// date must follow the start date. createEvent and updateEvent apply the same rules
// and select defect before any provider request.
func ValidateEventTimes(start EventDateTime, end EventDateTime) error {
	if err := validateEventBoundary("start", start); err != nil {
		return err
	}
	if err := validateEventBoundary("end", end); err != nil {
		return err
	}
	if start.IsAllDay() != end.IsAllDay() {
		return errors.New("start and end must both be all-day dates or both be timed dateTime values")
	}
	if start.IsAllDay() {
		startDate, _ := time.Parse(allDayDateLayout, start.Date)
		endDate, _ := time.Parse(allDayDateLayout, end.Date)
		if !endDate.After(startDate) {
			return fmt.Errorf("all-day end date %s is exclusive and must follow start date %s", end.Date, start.Date)
		}
		return nil
	}
	startInstant, _ := parseInstant("start.dateTime", start.DateTime)
	endInstant, _ := parseInstant("end.dateTime", end.DateTime)
	if !endInstant.After(startInstant) {
		return fmt.Errorf("end %s must be after start %s (%s and %s in UTC)", end.DateTime, start.DateTime,
			endInstant.UTC().Format(time.RFC3339), startInstant.UTC().Format(time.RFC3339))
	}
	return nil
}

func validateEventBoundary(name string, boundary EventDateTime) error {
	switch {
	case boundary.Date != "" && boundary.DateTime != "":
		return fmt.Errorf("%s sets both date and dateTime; set date for an all-day event or dateTime for a timed event", name)
	case boundary.Date != "":
		if _, err := time.Parse(allDayDateLayout, boundary.Date); err != nil {
			return fmt.Errorf("%s.date %q must use YYYY-MM-DD", name, boundary.Date)
		}
		if boundary.TimeZone != "" {
			return fmt.Errorf("%s is an all-day date, which has no time zone; remove %s.timeZone or use dateTime", name, name)
		}
		return nil
	case boundary.DateTime != "":
		if _, err := parseInstant(name+".dateTime", boundary.DateTime); err != nil {
			return err
		}
		if boundary.TimeZone == "" {
			return fmt.Errorf("%s.timeZone is required for a timed event; use an IANA name such as America/Los_Angeles", name)
		}
		return validateTimeZoneName(name+".timeZone", boundary.TimeZone)
	default:
		return fmt.Errorf("%s must set date for an all-day event or dateTime for a timed event", name)
	}
}

// parseInstant accepts only RFC 3339 values that carry a Z or ±hh:mm offset.
func parseInstant(name string, value string) (time.Time, error) {
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q must be RFC 3339 with an explicit offset, such as 2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z", name, value)
	}
	return instant, nil
}

func validateTimeZoneName(name string, value string) error {
	if value != strings.TrimSpace(value) || value == "" || value == "Local" {
		return fmt.Errorf("%s %q must be an IANA time zone name such as America/Los_Angeles", name, value)
	}
	if _, err := time.LoadLocation(value); err != nil {
		return fmt.Errorf("%s %q is not a known IANA time zone name", name, value)
	}
	return nil
}

// parseQueryWindow validates the required timeMin and timeMax bounds of a read.
func parseQueryWindow(timeMin string, timeMax string) (time.Time, time.Time, error) {
	if timeMin == "" || timeMax == "" {
		return time.Time{}, time.Time{}, errors.New("timeMin and timeMax are both required")
	}
	start, err := parseInstant("timeMin", timeMin)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseInstant("timeMax", timeMax)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	if !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("timeMax %s must be after timeMin %s", timeMax, timeMin)
	}
	if end.Sub(start) > maximumQueryWindow {
		return time.Time{}, time.Time{}, errors.New("the timeMin to timeMax window cannot exceed 366 days")
	}
	return start, end, nil
}

// isSameEventBoundary compares boundaries by instant and zone, so equal instants in different offsets match.
func isSameEventBoundary(current EventDateTime, requested EventDateTime) bool {
	if current.IsAllDay() || requested.IsAllDay() {
		return current.IsAllDay() && requested.IsAllDay() && current.Date == requested.Date
	}
	currentInstant, currentErr := parseInstant("dateTime", current.DateTime)
	requestedInstant, requestedErr := parseInstant("dateTime", requested.DateTime)
	return currentErr == nil && requestedErr == nil && currentInstant.Equal(requestedInstant) && current.TimeZone == requested.TimeZone
}
