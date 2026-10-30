// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package bookmeeting demonstrates every Outlook Calendar operation in one Flow started from
// Dex Web Start Flow: check the attendees' free/busy, place an uninvited hold under the Step's
// idempotency key, read it back, list the window for a double booking, and only then invite.
package bookmeeting

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "OutlookCalendarBookMeeting"
	// ConnectionName is the static Dex Web connection for Outlook Calendar.
	ConnectionName = "outlook-calendar-scheduler"

	recordMeetingRequestStepType         = "RecordMeetingRequest"
	checkAttendeeAvailabilityStepType    = "CheckAttendeeAvailability"
	evaluateAttendeeAvailabilityStepType = "EvaluateAttendeeAvailability"
	placeMeetingHoldStepType             = "PlaceMeetingHold"
	readBackMeetingHoldStepType          = "ReadBackMeetingHold"
	verifyMeetingHoldStepType            = "VerifyMeetingHold"
	listMeetingWindowEventsStepType      = "ListMeetingWindowEvents"
	evaluateMeetingWindowStepType        = "EvaluateMeetingWindow"
	inviteMeetingAttendeesStepType       = "InviteMeetingAttendees"
	completeMeetingBookingStepType       = "CompleteMeetingBooking"

	maximumListedWindowEvents = 250
	// maximumAttendees is getSchedule's documented limit of 20 schedules per request.
	maximumAttendees = 20
)

var (
	meetingRequestAttribute = dex.DefineAttribute[MeetingRequest]("outlook-calendar-meeting-request")
	meetingHoldAttribute    = dex.DefineAttribute[outlookcalendar.Event]("outlook-calendar-meeting-hold")
	meetingOutcomeAttribute = dex.DefineAttribute[MeetingOutcome]("outlook-calendar-meeting-outcome")
)

// Input is the meeting entered in Dex Web Start Flow. Start and End carry an explicit
// offset and an IANA time zone.
type Input struct {
	// Subject is the meeting title.
	Subject string `json:"subject"`
	// Body is the optional plain-text meeting body.
	Body string `json:"body,omitempty"`
	// Start is the meeting start, such as {"dateTime":"2026-02-24T09:00:00-08:00","timeZone":"America/Los_Angeles"}.
	Start outlookcalendar.EventDateTime `json:"start"`
	// End is the meeting end in the same form as Start.
	End outlookcalendar.EventDateTime `json:"end"`
	// Attendees lists 1 to 20 bare attendee addresses, checked for free/busy and invited after the hold is confirmed.
	Attendees []string `json:"attendees"`
	// RequestOnlineMeeting attaches a Microsoft Teams meeting to the hold.
	RequestOnlineMeeting bool `json:"requestOnlineMeeting,omitempty"`
}

// CalendarSelection is the value the calendar picker saves for the PlaceMeetingHold Step.
type CalendarSelection struct {
	// CalendarID is the picked calendar ID; blank uses the mailbox's default calendar.
	CalendarID string `json:"calendarId"`
	// CalendarName is the picked calendar's display name.
	CalendarName string `json:"calendarName,omitempty"`
}

// MeetingRequest is the validated request every later Step reads.
type MeetingRequest struct {
	// CalendarID is the calendar recorded when the Flow started; blank is the default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// Subject is the meeting title.
	Subject string `json:"subject"`
	// Body is the meeting body.
	Body string `json:"body,omitempty"`
	// Start is the timed meeting start.
	Start outlookcalendar.EventDateTime `json:"start"`
	// End is the timed meeting end.
	End outlookcalendar.EventDateTime `json:"end"`
	// Attendees lists the attendees to check and invite.
	Attendees []string `json:"attendees"`
	// RequestOnlineMeeting attaches a Microsoft Teams meeting to the hold.
	RequestOnlineMeeting bool `json:"requestOnlineMeeting,omitempty"`
}

// MeetingInvitation identifies the confirmed hold and the attendees to add to it.
type MeetingInvitation struct {
	// EventID is the hold's Graph event ID.
	EventID string `json:"eventId"`
	// Attendees lists the attendees to invite.
	Attendees []string `json:"attendees"`
	// TimeZone renders the invited event's times.
	TimeZone string `json:"timeZone"`
}

// MeetingStatus is the Flow's terminal business outcome.
type MeetingStatus string

const (
	// MeetingScheduled means the hold was confirmed and the attendees were invited.
	MeetingScheduled MeetingStatus = "scheduled"
	// MeetingBusy means free/busy reported an attendee busy, so no event was created.
	MeetingBusy MeetingStatus = "busy"
	// MeetingConflict means another event blocks the window on the organizer's calendar; nobody was invited.
	MeetingConflict MeetingStatus = "conflict"
)

// MeetingOutcome is the Flow result and the value of its outcome Attribute.
type MeetingOutcome struct {
	// Status is the terminal business outcome.
	Status MeetingStatus `json:"status"`
	// CalendarID is the calendar that was checked; blank is the default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// Event is the invited event, or the uninvited hold for a conflict.
	Event *outlookcalendar.Event `json:"event,omitempty"`
	// BusyAttendees lists the attendees whose free/busy showed busy, tentative, or out-of-office time.
	BusyAttendees []string `json:"busyAttendees,omitempty"`
	// ConflictingEventIDs lists other events that block the meeting window.
	ConflictingEventIDs []string `json:"conflictingEventIds,omitempty"`
	// IsWindowTruncated reports that the window held more events than one page, which counts as a conflict.
	IsWindowTruncated bool `json:"isWindowTruncated,omitempty"`
}

// Flow books one meeting on one Outlook calendar.
type Flow struct {
	dex.FlowDefaults
	connection outlookcalendar.Connection
	calendarID string
}

// NewFlow binds the Outlook Calendar Connection and the picked calendar at registration time.
// A blank selection uses the mailbox's default calendar.
func NewFlow(connection outlookcalendar.Connection, selection CalendarSelection) *Flow {
	return &Flow{connection: connection, calendarID: strings.TrimSpace(selection.CalendarID)}
}

// CalendarSelectionConfigurationRef identifies the calendar picker value saved in Dex Web.
func CalendarSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: outlookcalendar.ConnectorID, ConnectionName: ConnectionName, OperationID: "createEvent",
		FlowType: FlowType, StepType: placeMeetingHoldStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Outlook Calendar connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordMeetingRequest{calendarID: flow.calendarID}),
		dex.DefineStep(outlookcalendar.NewQueryFreeBusyStep(outlookcalendar.QueryFreeBusyStepConfig[MeetingRequest]{
			StepType: checkAttendeeAvailabilityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-calendar", GroupLabel: "Outlook Calendar",
				Explanation: "Ask Microsoft Graph getSchedule whether any attendee is busy during the meeting window.",
			},
			Connection: flow.connection, MapToOperationInput: MapToQueryFreeBusyInput,
			Queried: sdkgo.GoTo(evaluateAttendeeAvailability{}),
		})),
		dex.DefineStep(evaluateAttendeeAvailability{}),
		dex.DefineStep(outlookcalendar.NewCreateEventStep(outlookcalendar.CreateEventStepConfig[MeetingRequest]{
			StepType: placeMeetingHoldStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-calendar", GroupLabel: "Outlook Calendar",
				Explanation: "Place an uninvited hold under the Step's transactionId, so a re-dispatched Step cannot double-book.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "meetingCalendar", UnitID: outlookcalendar.UIUnitCalendarPicker, Label: "Meeting calendar",
				Description: "Choose the calendar that receives the meeting; the list shows only calendars this connection can edit, and the later read-back, window check, and invite use the same calendar. Leave it unsaved to use the mailbox's default calendar. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: outlookcalendar.UICalendarPickerPortCalendarID, JSONPointer: "/calendarId"},
					{Port: outlookcalendar.UICalendarPickerPortCalendarName, JSONPointer: "/calendarName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateEventInput,
			Created: sdkgo.GoTo(sdkgo.StepRef[outlookcalendar.CreateEventResult](readBackMeetingHoldStepType)),
		})),
		dex.DefineStep(outlookcalendar.NewGetEventStep(outlookcalendar.GetEventStepConfig[outlookcalendar.CreateEventResult]{
			StepType: readBackMeetingHoldStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-calendar", GroupLabel: "Outlook Calendar",
				Explanation: "Read the hold back from Microsoft Graph to confirm its stored start and end.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetEventInput,
			Found: sdkgo.GoTo(verifyMeetingHold{}),
		})),
		dex.DefineStep(verifyMeetingHold{}),
		dex.DefineStep(outlookcalendar.NewListEventsStep(outlookcalendar.ListEventsStepConfig[MeetingRequest]{
			StepType: listMeetingWindowEventsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-calendar", GroupLabel: "Outlook Calendar",
				Explanation: "List the calendar view of the meeting window to catch an event that blocks the organizer.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListEventsInput,
			Listed: sdkgo.GoTo(evaluateMeetingWindow{}),
		})),
		dex.DefineStep(evaluateMeetingWindow{}),
		dex.DefineStep(outlookcalendar.NewUpdateEventStep(outlookcalendar.UpdateEventStepConfig[MeetingInvitation]{
			StepType: inviteMeetingAttendeesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "outlook-calendar", GroupLabel: "Outlook Calendar",
				Explanation: "Add the attendees, which makes Outlook send the invitations, only after the hold is confirmed.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateEventInput,
			Updated: sdkgo.GoTo(completeMeetingBooking{}),
		})),
		dex.DefineStep(completeMeetingBooking{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request, hold, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{meetingRequestAttribute, meetingHoldAttribute, meetingOutcomeAttribute}}
}

// GetDexSummary returns the meeting request and outcome.
//
// dex:field attribute-key:outlook-calendar-meeting-request value-type:json editable:false description:"Requested meeting"
// dex:field attribute-key:outlook-calendar-meeting-outcome value-type:json editable:false description:"Booking outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, _, outcome, err := meetingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"outlook-calendar-meeting-request": request,
		"outlook-calendar-meeting-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the meeting request, hold, and outcome.
//
// dex:field attribute-key:outlook-calendar-meeting-request value-type:json editable:false description:"Calendar, times, and attendees"
// dex:field attribute-key:outlook-calendar-meeting-hold value-type:json editable:false description:"Hold event read back from Microsoft Graph"
// dex:field attribute-key:outlook-calendar-meeting-outcome value-type:json editable:false description:"Status, event, busy attendees, or conflicting events"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, hold, outcome, err := meetingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"outlook-calendar-meeting-request": request,
		"outlook-calendar-meeting-hold":    hold,
		"outlook-calendar-meeting-outcome": outcome,
	}}, nil
}

// MapToQueryFreeBusyInput checks exactly the meeting window for every attendee.
func MapToQueryFreeBusyInput(request MeetingRequest) outlookcalendar.QueryFreeBusyInput {
	return outlookcalendar.QueryFreeBusyInput{
		Schedules: request.Attendees, TimeMin: request.Start.DateTime, TimeMax: request.End.DateTime, TimeZone: request.Start.TimeZone,
	}
}

// MapToCreateEventInput places the hold without attendees, so Outlook sends no invitation before it is confirmed.
// The transactionId stays blank, so the Step's Call ID deduplicates every attempt of this Step.
func MapToCreateEventInput(request MeetingRequest) outlookcalendar.CreateEventInput {
	return outlookcalendar.CreateEventInput{
		CalendarID: request.CalendarID, Subject: request.Subject, Body: request.Body,
		Start: request.Start, End: request.End, RequestOnlineMeeting: request.RequestOnlineMeeting,
	}
}

// MapToGetEventInput reads back the event the hold Step created or found under its idempotency key.
func MapToGetEventInput(result outlookcalendar.CreateEventResult) outlookcalendar.GetEventInput {
	return outlookcalendar.GetEventInput{EventID: result.Value.Event.ID, TimeZone: result.Value.Event.Start.TimeZone}
}

// MapToListEventsInput lists one bounded page of the meeting window.
func MapToListEventsInput(request MeetingRequest) outlookcalendar.ListEventsInput {
	return outlookcalendar.ListEventsInput{
		CalendarID: request.CalendarID, TimeMin: request.Start.DateTime, TimeMax: request.End.DateTime,
		PageSize: maximumListedWindowEvents, TimeZone: request.Start.TimeZone,
	}
}

// MapToUpdateEventInput replaces the hold's attendee list, which makes Outlook invite every new attendee.
func MapToUpdateEventInput(invitation MeetingInvitation) outlookcalendar.UpdateEventInput {
	attendees := make([]outlookcalendar.EventAttendeeInput, len(invitation.Attendees))
	for index, email := range invitation.Attendees {
		attendees[index] = outlookcalendar.EventAttendeeInput{Email: email}
	}
	return outlookcalendar.UpdateEventInput{EventID: invitation.EventID, Attendees: &attendees, TimeZone: invitation.TimeZone}
}

// FindConflictingEventIDs returns other events that block time inside [windowStart, windowEnd).
// Cancelled events and events shown as free or working elsewhere do not block; an unknown
// status does. The comparison uses instants, never the text of a subject or body.
func FindConflictingEventIDs(events []outlookcalendar.Event, holdEventID string, windowStart time.Time, windowEnd time.Time) ([]string, error) {
	var conflicting []string
	for _, event := range events {
		if event.ID == holdEventID || event.IsCancelled || event.ShowAs == "free" || event.ShowAs == "workingElsewhere" {
			continue
		}
		start, err := event.Start.Instant()
		if err != nil {
			return nil, fmt.Errorf("event %s start: %w", event.ID, err)
		}
		end, err := event.End.Instant()
		if err != nil {
			return nil, fmt.Errorf("event %s end: %w", event.ID, err)
		}
		if start.Before(windowEnd) && end.After(windowStart) {
			conflicting = append(conflicting, event.ID)
		}
	}
	return conflicting, nil
}

func meetingInspection(ctx dex.Context) (MeetingRequest, outlookcalendar.Event, MeetingOutcome, error) {
	request, err := optionalAttribute(ctx, meetingRequestAttribute)
	if err != nil {
		return MeetingRequest{}, outlookcalendar.Event{}, MeetingOutcome{}, err
	}
	hold, err := optionalAttribute(ctx, meetingHoldAttribute)
	if err != nil {
		return MeetingRequest{}, outlookcalendar.Event{}, MeetingOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, meetingOutcomeAttribute)
	if err != nil {
		return MeetingRequest{}, outlookcalendar.Event{}, MeetingOutcome{}, err
	}
	return request, hold, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Validate the timed meeting and record it with the picked calendar before calling Microsoft Graph."
type recordMeetingRequest struct {
	dex.StepDefaults
	calendarID string
}

func (recordMeetingRequest) GetStepType() string { return recordMeetingRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordMeetingRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordMeetingRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := buildMeetingRequest(step.calendarID, input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := meetingRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[MeetingRequest](checkAttendeeAvailabilityStepType), request), nil
}

func buildMeetingRequest(calendarID string, input Input) (MeetingRequest, error) {
	request := MeetingRequest{
		CalendarID: calendarID, Subject: strings.TrimSpace(input.Subject), Body: strings.TrimSpace(input.Body),
		Start: input.Start, End: input.End, RequestOnlineMeeting: input.RequestOnlineMeeting,
	}
	if request.Subject == "" {
		return MeetingRequest{}, errors.New("subject is required")
	}
	if err := outlookcalendar.ValidateEventTimes(input.Start, input.End, false); err != nil {
		return MeetingRequest{}, err
	}
	if len(input.Attendees) == 0 || len(input.Attendees) > maximumAttendees {
		return MeetingRequest{}, fmt.Errorf("attendees must list 1 to %d addresses", maximumAttendees)
	}
	seen := make(map[string]bool, len(input.Attendees))
	for _, attendee := range input.Attendees {
		address, err := mail.ParseAddress(strings.TrimSpace(attendee))
		if err != nil || address.Name != "" {
			return MeetingRequest{}, fmt.Errorf("attendee %q must be one bare email address", attendee)
		}
		if seen[strings.ToLower(address.Address)] {
			return MeetingRequest{}, fmt.Errorf("attendee %q is listed twice", attendee)
		}
		seen[strings.ToLower(address.Address)] = true
		request.Attendees = append(request.Attendees, address.Address)
	}
	return request, nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Complete as busy when an attendee has busy time, otherwise place the hold."
type evaluateAttendeeAvailability struct {
	dex.StepDefaultsNoWaitFor[outlookcalendar.QueryFreeBusyResult]
}

func (evaluateAttendeeAvailability) GetStepType() string { return evaluateAttendeeAvailabilityStepType }

func (evaluateAttendeeAvailability) Execute(ctx dex.Context, result outlookcalendar.QueryFreeBusyResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	var busyAttendees []string
	for _, schedule := range result.Value.Schedules {
		if schedule.IsBusy {
			busyAttendees = append(busyAttendees, schedule.ScheduleID)
		}
	}
	if len(busyAttendees) != 0 {
		outcome := MeetingOutcome{Status: MeetingBusy, CalendarID: request.CalendarID, BusyAttendees: busyAttendees}
		if err := meetingOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[MeetingRequest](placeMeetingHoldStepType), request), nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Confirm the read-back hold has the requested instants before listing the window."
type verifyMeetingHold struct {
	dex.StepDefaultsNoWaitFor[outlookcalendar.GetEventResult]
}

func (verifyMeetingHold) GetStepType() string { return verifyMeetingHoldStepType }

func (verifyMeetingHold) Execute(ctx dex.Context, result outlookcalendar.GetEventResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	hold := result.Value
	if err := verifyHoldMatchesRequest(hold, request); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := meetingHoldAttribute.Set(ctx, hold); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[MeetingRequest](listMeetingWindowEventsStepType), request), nil
}

func verifyHoldMatchesRequest(hold outlookcalendar.Event, request MeetingRequest) error {
	if hold.IsCancelled || hold.IsAllDay {
		return fmt.Errorf("read-back hold %s is cancelled or all-day", hold.ID)
	}
	for _, boundary := range []struct {
		name              string
		stored, requested outlookcalendar.EventDateTime
	}{{"start", hold.Start, request.Start}, {"end", hold.End, request.End}} {
		storedInstant, err := boundary.stored.Instant()
		if err != nil {
			return fmt.Errorf("read-back hold %s %s: %w", hold.ID, boundary.name, err)
		}
		requestedInstant, err := boundary.requested.Instant()
		if err != nil {
			return fmt.Errorf("requested %s: %w", boundary.name, err)
		}
		if !storedInstant.Equal(requestedInstant) {
			return fmt.Errorf("read-back hold %s %s is %s, not the requested %s", hold.ID, boundary.name,
				storedInstant.UTC().Format(time.RFC3339), requestedInstant.UTC().Format(time.RFC3339))
		}
	}
	return nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Complete as a conflict when another blocking event overlaps the hold, otherwise invite."
type evaluateMeetingWindow struct {
	dex.StepDefaultsNoWaitFor[outlookcalendar.ListEventsResult]
}

func (evaluateMeetingWindow) GetStepType() string { return evaluateMeetingWindowStepType }

func (evaluateMeetingWindow) Execute(ctx dex.Context, result outlookcalendar.ListEventsResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	hold, err := meetingHoldAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	windowStart, err := request.Start.Instant()
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	windowEnd, err := request.End.Instant()
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	conflicting, err := FindConflictingEventIDs(result.Value.Events, hold.ID, windowStart, windowEnd)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	isWindowTruncated := result.Value.NextPageToken != ""
	if len(conflicting) != 0 || isWindowTruncated {
		outcome := MeetingOutcome{
			Status: MeetingConflict, CalendarID: request.CalendarID, Event: &hold,
			ConflictingEventIDs: conflicting, IsWindowTruncated: isWindowTruncated,
		}
		if err := meetingOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[MeetingInvitation](inviteMeetingAttendeesStepType), MeetingInvitation{
		EventID: hold.ID, Attendees: request.Attendees, TimeZone: request.Start.TimeZone,
	}), nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Record the invited event as the scheduled outcome and complete the Flow."
type completeMeetingBooking struct {
	dex.StepDefaultsNoWaitFor[outlookcalendar.UpdateEventResult]
}

func (completeMeetingBooking) GetStepType() string { return completeMeetingBookingStepType }

func (completeMeetingBooking) Execute(ctx dex.Context, result outlookcalendar.UpdateEventResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	event := result.Value.Event
	outcome := MeetingOutcome{Status: MeetingScheduled, CalendarID: request.CalendarID, Event: &event}
	if err := meetingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
