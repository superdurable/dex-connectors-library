// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DefaultTriggerCalendarID is the calendar an eventChanged binding watches when it saves no calendar.
const DefaultTriggerCalendarID = "primary"

// createdUpdatedTolerance is how close Google's created and updated times must be for a change to count as
// a creation. Google reports no change type, so the kind is derived from these timestamps.
const createdUpdatedTolerance = time.Second

// EventChangeKind is the derived kind of one eventChanged delivery.
type EventChangeKind string

const (
	// EventChangeCreated is a new event: Google's updated time is within one second of its created time.
	EventChangeCreated EventChangeKind = "created"
	// EventChangeUpdated is any other change to an event that is not cancelled, including a reschedule, a
	// guest change, or an edit made soon after creation but before the next poll read it.
	EventChangeUpdated EventChangeKind = "updated"
	// EventChangeCancelled is a deleted event or a cancelled instance of a recurring event.
	EventChangeCancelled EventChangeKind = "cancelled"
)

// EventChangedTriggerConfiguration is the eventChanged binding configuration that the calendar picker saves
// in Dex Web. Changing the calendar of a binding that already polled makes its next poll record a new
// baseline for the new calendar.
type EventChangedTriggerConfiguration struct {
	// CalendarID is the watched calendar ID from the calendar picker, such as
	// team@group.calendar.google.com; blank watches DefaultTriggerCalendarID, the authorized account's
	// primary calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// CalendarName is the picked calendar's display name, kept for display only.
	CalendarName string `json:"calendarName,omitempty"`
}

// Validate rejects a calendar ID with surrounding whitespace; blank is valid.
func (configuration EventChangedTriggerConfiguration) Validate() error {
	if configuration.CalendarID != strings.TrimSpace(configuration.CalendarID) {
		return errors.New("eventChanged calendarId must not have surrounding whitespace")
	}
	return nil
}

// WatchedCalendarID returns CalendarID, or DefaultTriggerCalendarID when it is blank.
func (configuration EventChangedTriggerConfiguration) WatchedCalendarID() string {
	if configuration.CalendarID == "" {
		return DefaultTriggerCalendarID
	}
	return configuration.CalendarID
}

// EventChangedEvent is one created, updated, or cancelled event read through incremental sync. Recurring
// events are not expanded: a change to a series arrives once for the series, and a changed or cancelled
// single instance arrives as its own event with RecurringEventID and OriginalStartTime set.
type EventChangedEvent struct {
	// CalendarID is the watched calendar.
	CalendarID string `json:"calendarId"`
	// ChangeKind is the derived change kind; see EventChangeKind.
	ChangeKind EventChangeKind `json:"changeKind"`
	// Event is the event as Google returned it in the sync page. A cancelled event is only guaranteed to
	// carry ID, and a cancelled recurring instance also RecurringEventID and OriginalStartTime.
	Event Event `json:"event"`
}

// newEventChangedTriggerEvent converts one sync item. Its ID is a pure function of the event version, so a
// refetch after a crash yields the same ID.
func newEventChangedTriggerEvent(calendarID string, event Event, now time.Time) sdkgo.TriggerEvent[EventChangedEvent] {
	return sdkgo.TriggerEvent[EventChangedEvent]{
		ID:         eventChangeID(calendarID, event),
		OccurredAt: eventChangeTime(event, now),
		Payload:    EventChangedEvent{CalendarID: calendarID, ChangeKind: eventChangeKind(event), Event: event},
	}
}

// eventChangeID hashes the event identity and version, so it stays short for any event ID length.
func eventChangeID(calendarID string, event Event) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{calendarID, event.ID, event.ETag, event.Updated, event.Status}, "\x00")))
	return "gcal-" + hex.EncodeToString(digest[:16])
}

func eventChangeKind(event Event) EventChangeKind {
	if event.Status == cancelledEventStatus {
		return EventChangeCancelled
	}
	created, createdErr := time.Parse(time.RFC3339Nano, event.Created)
	updated, updatedErr := time.Parse(time.RFC3339Nano, event.Updated)
	if createdErr == nil && updatedErr == nil && updated.Sub(created).Abs() <= createdUpdatedTolerance {
		return EventChangeCreated
	}
	return EventChangeUpdated
}

func eventChangeTime(event Event, now time.Time) time.Time {
	for _, value := range []string{event.Updated, event.Created} {
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed.UTC()
		}
	}
	return now.UTC()
}

func eventChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("google-calendar eventChanged configuration: %w", err)
}
