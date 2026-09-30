// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

const (
	// defaultTransparency is Google's documented value when an event omits transparency.
	defaultTransparency  = "opaque"
	cancelledEventStatus = "cancelled"
)

// Google Calendar accepts client-supplied event IDs of 5 to 1024 base32hex characters.
var eventIDPattern = regexp.MustCompile(`^[a-v0-9]+$`)

// Event is one Google Calendar event as returned by listEvents, getEvent, createEvent, and updateEvent.
// Provider enums such as Status, Transparency, and attendee ResponseStatus keep Google's own values.
type Event struct {
	// ID is the Google Calendar event ID; a recurring instance ID also encodes its original start.
	ID string `json:"id"`
	// CalendarID is the calendar the event was read from or written to.
	CalendarID string `json:"calendarId"`
	// Status is Google's status: confirmed, tentative, or cancelled.
	Status string `json:"status"`
	// Summary is the event title.
	Summary string `json:"summary,omitempty"`
	// Description is the event description, which may contain HTML.
	Description string `json:"description,omitempty"`
	// Location is the free-text event location.
	Location string `json:"location,omitempty"`
	// Start is the inclusive start; see EventDateTime. It is empty only for some cancelled events.
	Start EventDateTime `json:"start"`
	// End is the exclusive end; see EventDateTime. It is empty only for some cancelled events.
	End EventDateTime `json:"end"`
	// IsAllDay reports that Start and End are dates rather than instants.
	IsAllDay bool `json:"isAllDay"`
	// Transparency is opaque when the event blocks time or transparent when it does not.
	// Google omits the field for opaque events; the connector reports opaque explicitly.
	Transparency string `json:"transparency"`
	// Visibility is Google's visibility value, such as default, public, or private.
	Visibility string `json:"visibility,omitempty"`
	// Attendees lists the event guests in Google's order.
	Attendees []EventAttendee `json:"attendees,omitempty"`
	// OrganizerEmail is the organizer's calendar address.
	OrganizerEmail string `json:"organizerEmail,omitempty"`
	// RecurringEventID is the parent recurring event ID of an expanded instance.
	RecurringEventID string `json:"recurringEventId,omitempty"`
	// OriginalStartTime is the scheduled start of a recurring instance before any change.
	OriginalStartTime *EventDateTime `json:"originalStartTime,omitempty"`
	// Conference describes attached conferencing, such as a Google Meet link.
	Conference *EventConference `json:"conference,omitempty"`
	// HTMLLink opens the event in the Google Calendar web UI.
	HTMLLink string `json:"htmlLink,omitempty"`
	// ICalUID is the RFC 5545 identifier shared by every instance of a recurring event.
	ICalUID string `json:"iCalUID,omitempty"`
	// ETag is the event version used for conditional updates.
	ETag string `json:"etag,omitempty"`
	// Created is the RFC 3339 creation time reported by Google.
	Created string `json:"created,omitempty"`
	// Updated is the RFC 3339 last-modification time reported by Google.
	Updated string `json:"updated,omitempty"`
}

// EventAttendee is one guest of an event.
type EventAttendee struct {
	// Email is the guest's email address.
	Email string `json:"email"`
	// DisplayName is the guest's name when Google knows it.
	DisplayName string `json:"displayName,omitempty"`
	// ResponseStatus is Google's value: needsAction, declined, tentative, or accepted.
	ResponseStatus string `json:"responseStatus,omitempty"`
	// IsOptional reports an optional guest.
	IsOptional bool `json:"isOptional"`
	// IsOrganizer reports the event organizer.
	IsOrganizer bool `json:"isOrganizer"`
	// IsSelf reports the guest whose calendar was read.
	IsSelf bool `json:"isSelf"`
	// IsResource reports a room or other resource.
	IsResource bool `json:"isResource"`
}

// EventAttendeeInput is one guest supplied to createEvent or updateEvent.
type EventAttendeeInput struct {
	// Email is one bare address such as person@example.com, without a display name.
	Email string `json:"email"`
	// IsOptional marks the guest as optional.
	IsOptional bool `json:"isOptional,omitempty"`
}

// EventConference describes the conference attached to an event.
type EventConference struct {
	// SolutionType is Google's conference solution key, such as hangoutsMeet.
	SolutionType string `json:"solutionType,omitempty"`
	// ConferenceID is the provider conference ID.
	ConferenceID string `json:"conferenceId,omitempty"`
	// JoinURL is the video entry point URL.
	JoinURL string `json:"joinUrl,omitempty"`
	// CreateRequestStatus is pending, success, or failure after createEvent requests a conference.
	// Google creates the conference asynchronously, so pending may precede a JoinURL.
	CreateRequestStatus string `json:"createRequestStatus,omitempty"`
}

type googleEvent struct {
	ID                string            `json:"id"`
	Status            string            `json:"status"`
	Summary           string            `json:"summary"`
	Description       string            `json:"description"`
	Location          string            `json:"location"`
	Start             *EventDateTime    `json:"start"`
	End               *EventDateTime    `json:"end"`
	Transparency      string            `json:"transparency"`
	Visibility        string            `json:"visibility"`
	Attendees         []googleAttendee  `json:"attendees"`
	Organizer         *googlePerson     `json:"organizer"`
	RecurringEventID  string            `json:"recurringEventId"`
	OriginalStartTime *EventDateTime    `json:"originalStartTime"`
	ConferenceData    *googleConference `json:"conferenceData"`
	HTMLLink          string            `json:"htmlLink"`
	ICalUID           string            `json:"iCalUID"`
	ETag              string            `json:"etag"`
	Created           string            `json:"created"`
	Updated           string            `json:"updated"`
}

type googleAttendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	ResponseStatus string `json:"responseStatus"`
	Optional       bool   `json:"optional"`
	Organizer      bool   `json:"organizer"`
	Self           bool   `json:"self"`
	Resource       bool   `json:"resource"`
}

type googlePerson struct {
	Email string `json:"email"`
}

type googleConference struct {
	ConferenceID       string `json:"conferenceId"`
	ConferenceSolution *struct {
		Key struct {
			Type string `json:"type"`
		} `json:"key"`
	} `json:"conferenceSolution"`
	CreateRequest *struct {
		Status struct {
			StatusCode string `json:"statusCode"`
		} `json:"status"`
	} `json:"createRequest"`
	EntryPoints []struct {
		EntryPointType string `json:"entryPointType"`
		URI            string `json:"uri"`
	} `json:"entryPoints"`
}

type googleAttendeeRequest struct {
	Email    string `json:"email"`
	Optional bool   `json:"optional,omitempty"`
}

// decodeEvent strictly converts one Google event body into the public Event shape.
func decodeEvent(body []byte, calendarID string) (Event, error) {
	var resource googleEvent
	if err := json.Unmarshal(body, &resource); err != nil {
		return Event{}, errors.New("event response is not valid JSON")
	}
	return convertEvent(resource, calendarID)
}

func convertEvent(resource googleEvent, calendarID string) (Event, error) {
	if resource.ID == "" {
		return Event{}, errors.New("event response has no ID")
	}
	event := Event{
		ID: resource.ID, CalendarID: calendarID, Status: resource.Status, Summary: resource.Summary,
		Description: resource.Description, Location: resource.Location, Transparency: resource.Transparency,
		Visibility: resource.Visibility, RecurringEventID: resource.RecurringEventID,
		OriginalStartTime: resource.OriginalStartTime, HTMLLink: resource.HTMLLink, ICalUID: resource.ICalUID,
		ETag: resource.ETag, Created: resource.Created, Updated: resource.Updated,
	}
	if event.Transparency == "" {
		event.Transparency = defaultTransparency
	}
	if resource.Start != nil {
		event.Start = *resource.Start
	}
	if resource.End != nil {
		event.End = *resource.End
	}
	if resource.Status != cancelledEventStatus || resource.Start != nil || resource.End != nil {
		if err := validateReturnedEventTimes(event.Start, event.End); err != nil {
			return Event{}, fmt.Errorf("event %s: %w", resource.ID, err)
		}
	}
	event.IsAllDay = event.Start.IsAllDay()
	if resource.Organizer != nil {
		event.OrganizerEmail = resource.Organizer.Email
	}
	for _, attendee := range resource.Attendees {
		event.Attendees = append(event.Attendees, EventAttendee{
			Email: attendee.Email, DisplayName: attendee.DisplayName, ResponseStatus: attendee.ResponseStatus,
			IsOptional: attendee.Optional, IsOrganizer: attendee.Organizer, IsSelf: attendee.Self, IsResource: attendee.Resource,
		})
	}
	event.Conference = convertConference(resource.ConferenceData)
	return event, nil
}

// validateReturnedEventTimes requires each returned boundary to be one unambiguous date or offset instant.
func validateReturnedEventTimes(start EventDateTime, end EventDateTime) error {
	if err := validateReturnedEventBoundary("start", start); err != nil {
		return err
	}
	if err := validateReturnedEventBoundary("end", end); err != nil {
		return err
	}
	if start.IsAllDay() != end.IsAllDay() {
		return errors.New("start and end mix an all-day date with a timed dateTime")
	}
	return nil
}

func validateReturnedEventBoundary(name string, boundary EventDateTime) error {
	if (boundary.Date == "") == (boundary.DateTime == "") {
		return fmt.Errorf("%s must set exactly one of date and dateTime", name)
	}
	if boundary.IsAllDay() {
		if _, err := time.Parse(allDayDateLayout, boundary.Date); err != nil {
			return fmt.Errorf("%s.date %q is not YYYY-MM-DD", name, boundary.Date)
		}
		return nil
	}
	_, err := parseInstant(name+".dateTime", boundary.DateTime)
	return err
}

func convertConference(resource *googleConference) *EventConference {
	if resource == nil {
		return nil
	}
	conference := &EventConference{ConferenceID: resource.ConferenceID}
	if resource.ConferenceSolution != nil {
		conference.SolutionType = resource.ConferenceSolution.Key.Type
	}
	if resource.CreateRequest != nil {
		conference.CreateRequestStatus = resource.CreateRequest.Status.StatusCode
	}
	for _, entryPoint := range resource.EntryPoints {
		if entryPoint.EntryPointType == "video" {
			conference.JoinURL = entryPoint.URI
			break
		}
	}
	return conference
}

// validateAttendeeInputs requires unique bare addresses so a guest list has one meaning.
func validateAttendeeInputs(attendees []EventAttendeeInput) error {
	seen := make(map[string]bool, len(attendees))
	for _, attendee := range attendees {
		address, err := mail.ParseAddress(attendee.Email)
		if err != nil || address.Name != "" || address.Address != attendee.Email {
			return fmt.Errorf("attendee email %q must be one bare address such as person@example.com", attendee.Email)
		}
		canonical := strings.ToLower(address.Address)
		if seen[canonical] {
			return fmt.Errorf("attendee email %q is duplicated", attendee.Email)
		}
		seen[canonical] = true
	}
	return nil
}

func encodeAttendees(attendees []EventAttendeeInput) []googleAttendeeRequest {
	encoded := make([]googleAttendeeRequest, len(attendees))
	for index, attendee := range attendees {
		encoded[index] = googleAttendeeRequest{Email: attendee.Email, Optional: attendee.IsOptional}
	}
	return encoded
}

// hasSameAttendees compares guest sets by lowercase address and optional flag, ignoring order.
func hasSameAttendees(current []EventAttendee, requested []EventAttendeeInput) bool {
	if len(current) != len(requested) {
		return false
	}
	requestedOptional := make(map[string]bool, len(requested))
	for _, attendee := range requested {
		requestedOptional[strings.ToLower(attendee.Email)] = attendee.IsOptional
	}
	for _, attendee := range current {
		isOptional, ok := requestedOptional[strings.ToLower(attendee.Email)]
		if !ok || isOptional != attendee.IsOptional {
			return false
		}
	}
	return true
}

// encodeEventBoundary sends explicit nulls so a patch that switches timed and all-day clears the other form.
func encodeEventBoundary(boundary EventDateTime) map[string]any {
	encoded := map[string]any{"date": nil, "dateTime": nil, "timeZone": nil}
	if boundary.IsAllDay() {
		encoded["date"] = boundary.Date
		return encoded
	}
	encoded["dateTime"] = boundary.DateTime
	encoded["timeZone"] = boundary.TimeZone
	return encoded
}

func validateCalendarID(calendarID string) error {
	if strings.TrimSpace(calendarID) == "" || calendarID != strings.TrimSpace(calendarID) {
		return errors.New("calendarId is required, such as primary or a calendar ID from the calendar picker")
	}
	return nil
}

// validateClientEventID checks an ID createEvent supplies; Google-assigned and instance IDs use a wider alphabet.
func validateClientEventID(eventID string) error {
	if len(eventID) < 5 || len(eventID) > 1024 || !eventIDPattern.MatchString(eventID) {
		return errors.New("eventId must be 5 to 1024 lowercase base32hex characters (a-v and 0-9)")
	}
	return nil
}

func validateExistingEventID(eventID string) error {
	if strings.TrimSpace(eventID) == "" || eventID != strings.TrimSpace(eventID) {
		return errors.New("eventId is required")
	}
	return nil
}

func validateSendUpdates(sendUpdates string) error {
	switch sendUpdates {
	case "", "none", "all", "externalOnly":
		return nil
	default:
		return fmt.Errorf("sendUpdates %q must be blank, none, all, or externalOnly", sendUpdates)
	}
}
