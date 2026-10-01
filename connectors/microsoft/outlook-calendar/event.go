// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

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
	// unknownShowAs reports an event whose free/busy status Graph omitted, which callers must treat as blocking.
	unknownShowAs = "unknown"
	// idempotencyMarkerPropertyID is stamped on every created event, so a retry finds it by documented $filter.
	idempotencyMarkerPropertyID = "String {e8548d82-fea6-423a-bad8-7b6da468e968} Name DexIdempotencyKey"
	// eventSelectFields bounds every event read; createdDateTime and lastModifiedDateTime do not support $select.
	eventSelectFields = "id,subject,body,bodyPreview,start,end,originalStartTimeZone,originalEndTimeZone,isAllDay," +
		"isCancelled,isOrganizer,showAs,sensitivity,type,seriesMasterId,location,attendees,organizer,isOnlineMeeting," +
		"onlineMeetingProvider,onlineMeeting,webLink,iCalUId,changeKey,transactionId,responseStatus"
	// listEventSelectFields omits the full body so a page stays small; bodyPreview remains.
	listEventSelectFields = "id,subject,bodyPreview,start,end,originalStartTimeZone,originalEndTimeZone,isAllDay," +
		"isCancelled,isOrganizer,showAs,sensitivity,type,seriesMasterId,location,attendees,organizer,isOnlineMeeting," +
		"onlineMeetingProvider,onlineMeeting,webLink,iCalUId,changeKey,transactionId,responseStatus"
	teamsOnlineMeetingProvider = "teamsForBusiness"
)

// transactionIDPattern keeps a caller-supplied transactionId safe inside an OData string literal.
var transactionIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// Event is one Outlook calendar event as returned by listEvents, getEvent, createEvent, and updateEvent.
// Provider enums such as ShowAs, Type, Sensitivity, and attendee ResponseStatus keep Graph's own values.
type Event struct {
	// ID is the Graph event ID, unique within the mailbox; an expanded occurrence has its own ID.
	ID string `json:"id"`
	// CalendarID is the calendar the event was listed from or created in; blank when the operation did not name one.
	CalendarID string `json:"calendarId,omitempty"`
	// Subject is the event title.
	Subject string `json:"subject,omitempty"`
	// Body is the plain-text event body; listEvents omits it and returns BodyPreview instead.
	Body string `json:"body,omitempty"`
	// BodyPreview is Graph's first 255 characters of the body as text.
	BodyPreview string `json:"bodyPreview,omitempty"`
	// Location is the location display name.
	Location string `json:"location,omitempty"`
	// Start is the inclusive start in the requested display zone; see EventDateTime.
	Start EventDateTime `json:"start"`
	// End is the exclusive end in the requested display zone; see EventDateTime.
	End EventDateTime `json:"end"`
	// OriginalStartTimeZone is the zone the event was created in, as Graph reports it, which may be a Windows zone name.
	OriginalStartTimeZone string `json:"originalStartTimeZone,omitempty"`
	// OriginalEndTimeZone is the end's creation zone, as Graph reports it.
	OriginalEndTimeZone string `json:"originalEndTimeZone,omitempty"`
	// IsAllDay reports an all-day event; Start and End are then midnights in the event's own zone.
	IsAllDay bool `json:"isAllDay"`
	// IsCancelled reports a meeting the organizer cancelled that still shows on this calendar.
	IsCancelled bool `json:"isCancelled"`
	// IsOrganizer reports that the calendar's owner organizes the event.
	IsOrganizer bool `json:"isOrganizer"`
	// ShowAs is free, tentative, busy, oof, workingElsewhere, or unknown; unknown includes an omitted value.
	ShowAs string `json:"showAs"`
	// Sensitivity is normal, personal, private, or confidential.
	Sensitivity string `json:"sensitivity,omitempty"`
	// Type is singleInstance, occurrence, exception, or seriesMaster.
	Type string `json:"type,omitempty"`
	// SeriesMasterID is the recurring series an occurrence or exception belongs to.
	SeriesMasterID string `json:"seriesMasterId,omitempty"`
	// ResponseStatus is the calendar owner's response, such as organizer, accepted, or notResponded.
	ResponseStatus string `json:"responseStatus,omitempty"`
	// Attendees lists the event attendees in Graph's order.
	Attendees []EventAttendee `json:"attendees,omitempty"`
	// OrganizerEmail is the organizer's SMTP address.
	OrganizerEmail string `json:"organizerEmail,omitempty"`
	// OnlineMeeting describes an attached online meeting, such as a Teams meeting.
	OnlineMeeting *EventOnlineMeeting `json:"onlineMeeting,omitempty"`
	// WebLink opens the event in Outlook on the web.
	WebLink string `json:"webLink,omitempty"`
	// ICalUID is the RFC 5545 identifier shared by every instance of a recurring event.
	ICalUID string `json:"iCalUId,omitempty"`
	// ChangeKey changes every time the event changes.
	ChangeKey string `json:"changeKey,omitempty"`
	// ETag is Graph's @odata.etag, which updateEvent sends as If-Match.
	ETag string `json:"etag,omitempty"`
	// TransactionID is the client transactionId set at creation; Graph returns it only when an app set it.
	TransactionID string `json:"transactionId,omitempty"`
}

// EventAttendee is one attendee of an event.
type EventAttendee struct {
	// Email is the attendee's SMTP address.
	Email string `json:"email"`
	// Name is the attendee's display name when Graph knows it.
	Name string `json:"name,omitempty"`
	// Type is required, optional, or resource.
	Type string `json:"type,omitempty"`
	// ResponseStatus is Graph's response value, such as none, accepted, declined, or tentativelyAccepted.
	ResponseStatus string `json:"responseStatus,omitempty"`
}

// EventAttendeeInput is one attendee supplied to createEvent or updateEvent.
type EventAttendeeInput struct {
	// Email is one bare SMTP address such as person@example.com, without a display name.
	Email string `json:"email"`
	// IsOptional makes the attendee optional instead of required.
	IsOptional bool `json:"isOptional,omitempty"`
}

// EventOnlineMeeting describes the online meeting attached to an event.
type EventOnlineMeeting struct {
	// Provider is Graph's onlineMeetingProvider, such as teamsForBusiness.
	Provider string `json:"provider,omitempty"`
	// JoinURL is the URL attendees open to join; Graph can add it shortly after creation.
	JoinURL string `json:"joinUrl,omitempty"`
}

type graphEvent struct {
	ETag                  string                 `json:"@odata.etag"`
	ID                    string                 `json:"id"`
	Subject               string                 `json:"subject"`
	Body                  *graphItemBody         `json:"body"`
	BodyPreview           string                 `json:"bodyPreview"`
	Start                 *graphDateTimeTimeZone `json:"start"`
	End                   *graphDateTimeTimeZone `json:"end"`
	OriginalStartTimeZone string                 `json:"originalStartTimeZone"`
	OriginalEndTimeZone   string                 `json:"originalEndTimeZone"`
	IsAllDay              bool                   `json:"isAllDay"`
	IsCancelled           bool                   `json:"isCancelled"`
	IsOrganizer           bool                   `json:"isOrganizer"`
	ShowAs                string                 `json:"showAs"`
	Sensitivity           string                 `json:"sensitivity"`
	Type                  string                 `json:"type"`
	SeriesMasterID        string                 `json:"seriesMasterId"`
	Location              *graphLocation         `json:"location"`
	Attendees             []graphAttendee        `json:"attendees"`
	Organizer             *graphRecipient        `json:"organizer"`
	IsOnlineMeeting       bool                   `json:"isOnlineMeeting"`
	OnlineMeetingProvider string                 `json:"onlineMeetingProvider"`
	OnlineMeeting         *graphOnlineMeeting    `json:"onlineMeeting"`
	WebLink               string                 `json:"webLink"`
	ICalUID               string                 `json:"iCalUId"`
	ChangeKey             string                 `json:"changeKey"`
	TransactionID         string                 `json:"transactionId"`
	ResponseStatus        *graphResponseStatus   `json:"responseStatus"`
}

type graphItemBody struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type graphLocation struct {
	DisplayName string `json:"displayName"`
}

type graphEmailAddress struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

type graphRecipient struct {
	EmailAddress *graphEmailAddress `json:"emailAddress"`
}

type graphAttendee struct {
	Type         string               `json:"type"`
	Status       *graphResponseStatus `json:"status"`
	EmailAddress *graphEmailAddress   `json:"emailAddress"`
}

type graphResponseStatus struct {
	Response string `json:"response"`
}

type graphOnlineMeeting struct {
	JoinURL string `json:"joinUrl"`
}

type graphAttendeeRequest struct {
	EmailAddress graphEmailAddress `json:"emailAddress"`
	Type         string            `json:"type"`
}

// eventDisplay is the zone returned event times are rendered in.
type eventDisplay struct {
	location *time.Location
	zone     string
}

// decodeEvent strictly converts one Graph event body into the public Event shape.
func decodeEvent(body []byte, calendarID string, display eventDisplay) (Event, error) {
	var resource graphEvent
	if err := json.Unmarshal(body, &resource); err != nil {
		return Event{}, errors.New("event response is not valid JSON")
	}
	return convertEvent(resource, calendarID, display)
}

func convertEvent(resource graphEvent, calendarID string, display eventDisplay) (Event, error) {
	if resource.ID == "" {
		return Event{}, errors.New("event response has no ID")
	}
	start, startInstant, err := decodeGraphBoundary("start", resource.Start, display.location, display.zone)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: %w", resource.ID, err)
	}
	end, endInstant, err := decodeGraphBoundary("end", resource.End, display.location, display.zone)
	if err != nil {
		return Event{}, fmt.Errorf("event %s: %w", resource.ID, err)
	}
	if endInstant.Before(startInstant) {
		return Event{}, fmt.Errorf("event %s ends before it starts", resource.ID)
	}
	event := Event{
		ID: resource.ID, CalendarID: calendarID, Subject: resource.Subject, BodyPreview: resource.BodyPreview,
		Start: start, End: end, OriginalStartTimeZone: resource.OriginalStartTimeZone,
		OriginalEndTimeZone: resource.OriginalEndTimeZone, IsAllDay: resource.IsAllDay, IsCancelled: resource.IsCancelled,
		IsOrganizer: resource.IsOrganizer, ShowAs: resource.ShowAs, Sensitivity: resource.Sensitivity, Type: resource.Type,
		SeriesMasterID: resource.SeriesMasterID, WebLink: resource.WebLink, ICalUID: resource.ICalUID,
		ChangeKey: resource.ChangeKey, ETag: resource.ETag, TransactionID: resource.TransactionID,
	}
	if event.ShowAs == "" {
		event.ShowAs = unknownShowAs
	}
	if resource.Body != nil {
		event.Body = resource.Body.Content
	}
	if resource.Location != nil {
		event.Location = resource.Location.DisplayName
	}
	if resource.ResponseStatus != nil {
		event.ResponseStatus = resource.ResponseStatus.Response
	}
	if resource.Organizer != nil && resource.Organizer.EmailAddress != nil {
		event.OrganizerEmail = resource.Organizer.EmailAddress.Address
	}
	for _, attendee := range resource.Attendees {
		if attendee.EmailAddress == nil {
			continue
		}
		converted := EventAttendee{Email: attendee.EmailAddress.Address, Name: attendee.EmailAddress.Name, Type: attendee.Type}
		if attendee.Status != nil {
			converted.ResponseStatus = attendee.Status.Response
		}
		event.Attendees = append(event.Attendees, converted)
	}
	if resource.IsOnlineMeeting || resource.OnlineMeeting != nil {
		event.OnlineMeeting = &EventOnlineMeeting{Provider: resource.OnlineMeetingProvider}
		if resource.OnlineMeeting != nil {
			event.OnlineMeeting.JoinURL = resource.OnlineMeeting.JoinURL
		}
	}
	return event, nil
}

// validateAttendeeInputs requires unique bare addresses so an attendee list has one meaning.
func validateAttendeeInputs(attendees []EventAttendeeInput) error {
	seen := make(map[string]bool, len(attendees))
	for _, attendee := range attendees {
		if err := validateBareEmailAddress("attendee email", attendee.Email); err != nil {
			return err
		}
		canonical := strings.ToLower(attendee.Email)
		if seen[canonical] {
			return fmt.Errorf("attendee email %q is duplicated", attendee.Email)
		}
		seen[canonical] = true
	}
	return nil
}

func validateBareEmailAddress(name string, value string) error {
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return fmt.Errorf("%s %q must be one bare address such as person@example.com", name, value)
	}
	return nil
}

func encodeAttendees(attendees []EventAttendeeInput) []graphAttendeeRequest {
	encoded := make([]graphAttendeeRequest, len(attendees))
	for index, attendee := range attendees {
		attendeeType := "required"
		if attendee.IsOptional {
			attendeeType = "optional"
		}
		encoded[index] = graphAttendeeRequest{EmailAddress: graphEmailAddress{Address: attendee.Email}, Type: attendeeType}
	}
	return encoded
}

// hasSameAttendees compares attendee sets by lowercase address and required or optional type, ignoring order.
func hasSameAttendees(current []EventAttendee, requested []EventAttendeeInput) bool {
	if len(current) != len(requested) {
		return false
	}
	requestedTypes := make(map[string]string, len(requested))
	for _, attendee := range encodeAttendees(requested) {
		requestedTypes[strings.ToLower(attendee.EmailAddress.Address)] = attendee.Type
	}
	for _, attendee := range current {
		attendeeType, ok := requestedTypes[strings.ToLower(attendee.Email)]
		if !ok || !strings.EqualFold(attendeeType, attendee.Type) {
			return false
		}
	}
	return true
}

// isSameBodyText compares bodies after the line-ending and edge-whitespace changes Outlook's text conversion makes.
func isSameBodyText(current string, requested string) bool {
	normalizeLines := func(text string) string {
		return strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	}
	return normalizeLines(current) == normalizeLines(requested)
}

func validateCalendarID(calendarID string) error {
	if calendarID != strings.TrimSpace(calendarID) {
		return errors.New("calendarId cannot have surrounding whitespace; leave it blank for the mailbox's default calendar")
	}
	return nil
}

func validateExistingEventID(eventID string) error {
	if strings.TrimSpace(eventID) == "" || eventID != strings.TrimSpace(eventID) {
		return errors.New("eventId is required")
	}
	return nil
}

// validateTransactionID checks a caller-supplied transactionId; the Call ID fallback is a UUID.
func validateTransactionID(transactionID string) error {
	if !transactionIDPattern.MatchString(transactionID) {
		return errors.New("transactionId must be 1 to 64 letters, digits, dots, hyphens, or underscores")
	}
	return nil
}
