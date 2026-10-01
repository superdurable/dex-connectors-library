// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"errors"
	"fmt"
	"strings"
	"time"

	// Embedded zone data makes IANA validation and conversion identical on hosts without a zoneinfo database.
	_ "time/tzdata"
)

const (
	// graphWallTimeLayout is Microsoft Graph's dateTimeTimeZone.dateTime form, a wall time without an offset.
	graphWallTimeLayout = "2006-01-02T15:04:05"
	graphUTCTimeZone    = "UTC"
	// maximumListWindow bounds the listEvents window.
	maximumListWindow = 366 * 24 * time.Hour
	// maximumFreeBusyWindow is Microsoft's documented getSchedule limit: the period must be less than 62 days.
	maximumFreeBusyWindow = 62 * 24 * time.Hour
)

// EventDateTime is one event boundary: an absolute instant plus the IANA time zone
// it is shown in.
//
// DateTime is RFC 3339 with an explicit Z or ±hh:mm offset, such as
// 2026-02-24T09:00:00-08:00, and TimeZone is an IANA name such as
// America/Los_Angeles. The offset fixes the instant; TimeZone is the zone the event
// is written in, so Outlook keeps the organizer's zone. A value without an offset,
// such as 2026-02-24T09:00:00, or without TimeZone, is rejected rather than guessed.
//
// Microsoft Graph itself stores a wall time without an offset plus a zone name. The
// connector writes the wall time of the instant in TimeZone, or the UTC wall time when a
// daylight-saving change makes that wall time ambiguous, so the instant never moves.
// It reads every time back in UTC and returns it with an offset in the requested zone.
type EventDateTime struct {
	// DateTime is the RFC 3339 instant with an explicit offset.
	DateTime string `json:"dateTime"`
	// TimeZone is the IANA time zone of the boundary, such as America/Los_Angeles.
	TimeZone string `json:"timeZone"`
}

// Instant returns the boundary as an absolute time.
func (boundary EventDateTime) Instant() (time.Time, error) {
	return parseInstant("dateTime", boundary.DateTime)
}

// ValidateEventTimes reports whether start and end describe one unambiguous event.
// Each boundary needs an RFC 3339 offset and an IANA TimeZone, and end must be a later
// instant than start even when the two offsets differ. When isAllDay is true, both
// boundaries must be midnight in the same TimeZone, as Microsoft Graph requires for an
// all-day event; the end is exclusive, so a one-day event on 24 February ends at
// midnight on 25 February. createEvent and updateEvent apply the same rules and select
// defect before any provider request.
func ValidateEventTimes(start EventDateTime, end EventDateTime, isAllDay bool) error {
	startInstant, err := validateEventBoundary("start", start)
	if err != nil {
		return err
	}
	endInstant, err := validateEventBoundary("end", end)
	if err != nil {
		return err
	}
	if !endInstant.After(startInstant) {
		return fmt.Errorf("end %s must be after start %s (%s and %s in UTC)", end.DateTime, start.DateTime,
			endInstant.UTC().Format(time.RFC3339), startInstant.UTC().Format(time.RFC3339))
	}
	if !isAllDay {
		return nil
	}
	if start.TimeZone != end.TimeZone {
		return errors.New("an all-day event needs start and end in the same timeZone")
	}
	for _, boundary := range []struct {
		name    string
		instant time.Time
		zone    string
	}{{"start", startInstant, start.TimeZone}, {"end", endInstant, end.TimeZone}} {
		location, _ := time.LoadLocation(boundary.zone)
		local := boundary.instant.In(location)
		if local.Hour() != 0 || local.Minute() != 0 || local.Second() != 0 || local.Nanosecond() != 0 {
			return fmt.Errorf("an all-day %s must be midnight in %s, not %s", boundary.name, boundary.zone, local.Format(time.RFC3339))
		}
		if isAmbiguousWallTime(boundary.instant, location) {
			return fmt.Errorf("an all-day %s falls on an ambiguous midnight in %s", boundary.name, boundary.zone)
		}
	}
	return nil
}

func validateEventBoundary(name string, boundary EventDateTime) (time.Time, error) {
	instant, err := parseInstant(name+".dateTime", boundary.DateTime)
	if err != nil {
		return time.Time{}, err
	}
	if boundary.TimeZone == "" {
		return time.Time{}, fmt.Errorf("%s.timeZone is required; use an IANA name such as America/Los_Angeles", name)
	}
	if err := validateTimeZoneName(name+".timeZone", boundary.TimeZone); err != nil {
		return time.Time{}, err
	}
	return instant, nil
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

// loadDisplayLocation returns the IANA zone for returned offsets; blank means UTC.
func loadDisplayLocation(timeZone string) (*time.Location, string, error) {
	if timeZone == "" {
		return time.UTC, graphUTCTimeZone, nil
	}
	if err := validateTimeZoneName("timeZone", timeZone); err != nil {
		return nil, "", err
	}
	location, err := time.LoadLocation(timeZone)
	return location, timeZone, err
}

// parseQueryWindow validates the required timeMin and timeMax bounds of a read; callers bound its length.
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
	return start, end, nil
}

// graphDateTimeTimeZone is Microsoft Graph's dateTimeTimeZone: a wall time and the zone it is read in.
type graphDateTimeTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

// encodeGraphBoundary writes the zone's wall time, or UTC when daylight saving makes it ambiguous.
func encodeGraphBoundary(boundary EventDateTime) graphDateTimeTimeZone {
	instant, _ := parseInstant("dateTime", boundary.DateTime)
	location, err := time.LoadLocation(boundary.TimeZone)
	if err != nil || isAmbiguousWallTime(instant, location) {
		return encodeGraphUTC(instant)
	}
	return graphDateTimeTimeZone{DateTime: instant.In(location).Format(graphWallTimeLayout), TimeZone: boundary.TimeZone}
}

func encodeGraphUTC(instant time.Time) graphDateTimeTimeZone {
	return graphDateTimeTimeZone{DateTime: instant.UTC().Format(graphWallTimeLayout), TimeZone: graphUTCTimeZone}
}

// isAmbiguousWallTime reports whether another instant within a day shares this instant's wall clock in location.
func isAmbiguousWallTime(instant time.Time, location *time.Location) bool {
	local := instant.In(location)
	wallTime := local.Format(graphWallTimeLayout)
	_, offsetSeconds := local.Zone()
	for _, probe := range []time.Duration{-12 * time.Hour, 12 * time.Hour} {
		_, probeOffsetSeconds := instant.Add(probe).In(location).Zone()
		if probeOffsetSeconds == offsetSeconds {
			continue
		}
		other := instant.Add(time.Duration(offsetSeconds-probeOffsetSeconds) * time.Second)
		if other.In(location).Format(graphWallTimeLayout) == wallTime {
			return true
		}
	}
	return false
}

// decodeGraphBoundary reads a returned dateTimeTimeZone, which the connector requests in UTC.
func decodeGraphBoundary(name string, value *graphDateTimeTimeZone, display *time.Location, displayZone string) (EventDateTime, time.Time, error) {
	if value == nil || value.DateTime == "" {
		return EventDateTime{}, time.Time{}, fmt.Errorf("%s is missing", name)
	}
	location, err := loadReturnedLocation(value.TimeZone)
	if err != nil {
		return EventDateTime{}, time.Time{}, fmt.Errorf("%s: %w", name, err)
	}
	instant, err := time.ParseInLocation(graphWallTimeLayout, value.DateTime, location)
	if err != nil {
		return EventDateTime{}, time.Time{}, fmt.Errorf("%s.dateTime is not a Graph wall time", name)
	}
	return EventDateTime{DateTime: instant.In(display).Format(time.RFC3339), TimeZone: displayZone}, instant, nil
}

// loadReturnedLocation accepts UTC, which the connector requests, or any IANA name; a Windows zone name is not convertible.
func loadReturnedLocation(timeZone string) (*time.Location, error) {
	switch timeZone {
	case "", graphUTCTimeZone, "Etc/UTC", "Etc/GMT":
		return time.UTC, nil
	}
	location, err := time.LoadLocation(timeZone)
	if err != nil || timeZone == "Local" {
		return nil, errors.New("returned time zone is neither UTC nor an IANA name")
	}
	return location, nil
}
