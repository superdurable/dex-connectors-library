// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package schedulemeeting demonstrates every Google Calendar operation in one Flow started
// from Dex Web Start Flow: check free/busy, place an idempotent hold, read it back, list the
// window for a double booking, and only then invite the attendees.
package schedulemeeting

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleCalendarScheduleMeeting"
	// ConnectionName is the static Dex Web connection for Google Calendar.
	ConnectionName = "google-calendar-scheduler"
	// DefaultCalendarID is used when no calendar has been picked in Dex Web.
	DefaultCalendarID = "primary"

	recordMeetingRequestStepType        = "RecordMeetingRequest"
	checkMeetingAvailabilityStepType    = "CheckMeetingAvailability"
	evaluateMeetingAvailabilityStepType = "EvaluateMeetingAvailability"
	placeMeetingHoldStepType            = "PlaceMeetingHold"
	readBackMeetingHoldStepType         = "ReadBackMeetingHold"
	verifyMeetingHoldStepType           = "VerifyMeetingHold"
	listMeetingWindowEventsStepType     = "ListMeetingWindowEvents"
	evaluateMeetingWindowStepType       = "EvaluateMeetingWindow"
	inviteMeetingAttendeesStepType      = "InviteMeetingAttendees"
	completeMeetingBookingStepType      = "CompleteMeetingBooking"

	maximumListedWindowEvents = 250
)

var (
	meetingRequestAttribute = dex.DefineAttribute[MeetingRequest]("google-calendar-meeting-request")
	meetingHoldAttribute    = dex.DefineAttribute[calendar.Event]("google-calendar-meeting-hold")
	meetingOutcomeAttribute = dex.DefineAttribute[MeetingOutcome]("google-calendar-meeting-outcome")
)

// Input is the meeting entered in Dex Web Start Flow. Start and End are timed
// boundaries with an explicit offset and an IANA time zone.
type Input struct {
	// Summary is the meeting title.
	Summary string `json:"summary"`
	// Description is the optional meeting description.
	Description string `json:"description,omitempty"`
	// Start is the meeting start, such as {"dateTime":"2026-02-24T09:00:00-08:00","timeZone":"America/Los_Angeles"}.
	Start calendar.EventDateTime `json:"start"`
	// End is the meeting end in the same form as Start.
	End calendar.EventDateTime `json:"end"`
	// Attendees lists one or more bare guest addresses invited after the hold is confirmed.
	Attendees []string `json:"attendees"`
	// RequestConference attaches a Google Meet conference to the hold.
	RequestConference bool `json:"requestConference,omitempty"`
}

// CalendarSelection is the value the calendar picker saves for the PlaceMeetingHold Step.
type CalendarSelection struct {
	// CalendarID is the picked calendar ID; blank uses DefaultCalendarID.
	CalendarID string `json:"calendarId"`
	// CalendarName is the picked calendar's display name.
	CalendarName string `json:"calendarName,omitempty"`
}

// MeetingRequest is the validated request every later Step reads.
type MeetingRequest struct {
	// CalendarID is the calendar recorded when the Flow started.
	CalendarID string `json:"calendarId"`
	// Summary is the meeting title.
	Summary string `json:"summary"`
	// Description is the meeting description.
	Description string `json:"description,omitempty"`
	// Start is the timed meeting start.
	Start calendar.EventDateTime `json:"start"`
	// End is the timed meeting end.
	End calendar.EventDateTime `json:"end"`
	// Attendees lists the guests to invite.
	Attendees []string `json:"attendees"`
	// RequestConference attaches a Google Meet conference to the hold.
	RequestConference bool `json:"requestConference,omitempty"`
}

// MeetingInvitation identifies the confirmed hold and the guests to add to it.
type MeetingInvitation struct {
	// CalendarID is the calendar that holds the event.
	CalendarID string `json:"calendarId"`
	// EventID is the hold's stable event ID.
	EventID string `json:"eventId"`
	// Attendees lists the guests to invite.
	Attendees []string `json:"attendees"`
}

// MeetingStatus is the Flow's terminal business outcome.
type MeetingStatus string

const (
	// MeetingScheduled means the hold was confirmed and the attendees were invited.
	MeetingScheduled MeetingStatus = "scheduled"
	// MeetingBusy means free/busy reported the calendar busy, so no event was created.
	MeetingBusy MeetingStatus = "busy"
	// MeetingConflict means another event appeared in the window after the hold; nobody was invited.
	MeetingConflict MeetingStatus = "conflict"
)

// MeetingOutcome is the Flow result and the value of its outcome Attribute.
type MeetingOutcome struct {
	// Status is the terminal business outcome.
	Status MeetingStatus `json:"status"`
	// CalendarID is the calendar that was checked.
	CalendarID string `json:"calendarId"`
	// Event is the invited event, or the uninvited hold for a conflict.
	Event *calendar.Event `json:"event,omitempty"`
	// Busy lists the busy intervals that prevented a hold.
	Busy []calendar.BusyInterval `json:"busy,omitempty"`
	// ConflictingEventIDs lists other events that block the meeting window.
	ConflictingEventIDs []string `json:"conflictingEventIds,omitempty"`
	// IsWindowTruncated reports that the window held more events than one page, which counts as a conflict.
	IsWindowTruncated bool `json:"isWindowTruncated,omitempty"`
}

// Flow schedules one meeting on one Google Calendar.
type Flow struct {
	dex.FlowDefaults
	connection calendar.Connection
	calendarID string
}

// NewFlow binds the Google Calendar Connection and the picked calendar at registration time.
// A blank selection uses DefaultCalendarID, the authorized account's primary calendar.
func NewFlow(connection calendar.Connection, selection CalendarSelection) *Flow {
	calendarID := strings.TrimSpace(selection.CalendarID)
	if calendarID == "" {
		calendarID = DefaultCalendarID
	}
	return &Flow{connection: connection, calendarID: calendarID}
}

// CalendarSelectionConfigurationRef identifies the calendar picker value saved in Dex Web.
func CalendarSelectionConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: calendar.ConnectorID, ConnectionName: ConnectionName, OperationID: "createEvent",
		FlowType: FlowType, StepType: placeMeetingHoldStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Google Calendar connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordMeetingRequest{calendarID: flow.calendarID}),
		dex.DefineStep(calendar.NewQueryFreeBusyStep(calendar.QueryFreeBusyStepConfig[MeetingRequest]{
			StepType: checkMeetingAvailabilityStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-calendar", GroupLabel: "Google Calendar",
				Explanation: "Ask Google Calendar whether the calendar is busy during the meeting window.",
			},
			Connection: flow.connection, MapToOperationInput: MapToQueryFreeBusyInput,
			Queried: sdkgo.GoTo(evaluateMeetingAvailability{}),
		})),
		dex.DefineStep(evaluateMeetingAvailability{}),
		dex.DefineStep(calendar.NewCreateEventStep(calendar.CreateEventStepConfig[MeetingRequest]{
			StepType: placeMeetingHoldStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-calendar", GroupLabel: "Google Calendar",
				Explanation: "Place an uninvited hold under a stable event ID, so a retried Step cannot double-book.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "meetingCalendar", UnitID: calendar.UIUnitCalendarPicker, Label: "Meeting calendar",
				Description: "Choose the calendar that receives the meeting; the list shows only calendars this connection can edit, and every later availability, read-back, and invite Step uses the same calendar. Leave it unsaved to use the authorized account's primary calendar. Restart the Worker after saving.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: calendar.UICalendarPickerPortCalendarID, JSONPointer: "/calendarId"},
					{Port: calendar.UICalendarPickerPortCalendarName, JSONPointer: "/calendarName"},
				},
			}}},
			Connection: flow.connection, MapToOperationInput: MapToCreateEventInput,
			Created: sdkgo.GoTo(sdkgo.StepRef[calendar.CreateEventResult](readBackMeetingHoldStepType)),
		})),
		dex.DefineStep(calendar.NewGetEventStep(calendar.GetEventStepConfig[calendar.CreateEventResult]{
			StepType: readBackMeetingHoldStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-calendar", GroupLabel: "Google Calendar",
				Explanation: "Read the hold back from Google Calendar to confirm its stored start and end.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetEventInput,
			Found: sdkgo.GoTo(verifyMeetingHold{}),
		})),
		dex.DefineStep(verifyMeetingHold{}),
		dex.DefineStep(calendar.NewListEventsStep(calendar.ListEventsStepConfig[MeetingRequest]{
			StepType: listMeetingWindowEventsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-calendar", GroupLabel: "Google Calendar",
				Explanation: "List the meeting window to catch an event booked after the free/busy check.",
			},
			Connection: flow.connection, MapToOperationInput: MapToListEventsInput,
			Listed: sdkgo.GoTo(evaluateMeetingWindow{}),
		})),
		dex.DefineStep(evaluateMeetingWindow{}),
		dex.DefineStep(calendar.NewUpdateEventStep(calendar.UpdateEventStepConfig[MeetingInvitation]{
			StepType: inviteMeetingAttendeesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-calendar", GroupLabel: "Google Calendar",
				Explanation: "Invite the attendees and send Google's invitations only after the hold is confirmed.",
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
// dex:field attribute-key:google-calendar-meeting-request value-type:json editable:false description:"Requested meeting"
// dex:field attribute-key:google-calendar-meeting-outcome value-type:json editable:false description:"Scheduling outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, _, outcome, err := meetingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-calendar-meeting-request": request,
		"google-calendar-meeting-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the meeting request, hold, and outcome.
//
// dex:field attribute-key:google-calendar-meeting-request value-type:json editable:false description:"Calendar, times, and attendees"
// dex:field attribute-key:google-calendar-meeting-hold value-type:json editable:false description:"Hold event read back from Google Calendar"
// dex:field attribute-key:google-calendar-meeting-outcome value-type:json editable:false description:"Status, event, busy intervals, or conflicting events"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, hold, outcome, err := meetingInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-calendar-meeting-request": request,
		"google-calendar-meeting-hold":    hold,
		"google-calendar-meeting-outcome": outcome,
	}}, nil
}

// MapToQueryFreeBusyInput checks exactly the meeting window on the recorded calendar.
func MapToQueryFreeBusyInput(request MeetingRequest) calendar.QueryFreeBusyInput {
	return calendar.QueryFreeBusyInput{
		CalendarIDs: []string{request.CalendarID}, TimeMin: request.Start.DateTime, TimeMax: request.End.DateTime,
		TimeZone: request.Start.TimeZone,
	}
}

// MapToCreateEventInput places the hold without guests, so nobody is notified before it is confirmed.
func MapToCreateEventInput(request MeetingRequest) calendar.CreateEventInput {
	return calendar.CreateEventInput{
		CalendarID: request.CalendarID, Summary: request.Summary, Description: request.Description,
		Start: request.Start, End: request.End, RequestConference: request.RequestConference, SendUpdates: "none",
	}
}

// MapToGetEventInput reads back the event the hold Step created or found under its stable ID.
func MapToGetEventInput(result calendar.CreateEventResult) calendar.GetEventInput {
	return calendar.GetEventInput{CalendarID: result.Value.Event.CalendarID, EventID: result.Value.Event.ID}
}

// MapToListEventsInput lists one bounded page of the meeting window.
func MapToListEventsInput(request MeetingRequest) calendar.ListEventsInput {
	return calendar.ListEventsInput{
		CalendarID: request.CalendarID, TimeMin: request.Start.DateTime, TimeMax: request.End.DateTime,
		PageSize: maximumListedWindowEvents, TimeZone: request.Start.TimeZone,
	}
}

// MapToUpdateEventInput replaces the hold's guest list and asks Google to email every guest.
func MapToUpdateEventInput(invitation MeetingInvitation) calendar.UpdateEventInput {
	attendees := make([]calendar.EventAttendeeInput, len(invitation.Attendees))
	for index, email := range invitation.Attendees {
		attendees[index] = calendar.EventAttendeeInput{Email: email}
	}
	return calendar.UpdateEventInput{
		CalendarID: invitation.CalendarID, EventID: invitation.EventID, Attendees: &attendees, SendUpdates: "all",
	}
}

// FindConflictingEventIDs returns other events that block time inside [windowStart, windowEnd).
// Transparent and cancelled events are free. An all-day date is read in allDayLocation,
// the calendar's zone. The comparison uses instants, never the text of a summary or description.
func FindConflictingEventIDs(events []calendar.Event, holdEventID string, windowStart time.Time, windowEnd time.Time, allDayLocation *time.Location) ([]string, error) {
	var conflicting []string
	for _, event := range events {
		if event.ID == holdEventID || event.Transparency == "transparent" || event.Status == "cancelled" {
			continue
		}
		start, err := event.Start.Instant(allDayLocation)
		if err != nil {
			return nil, fmt.Errorf("event %s start: %w", event.ID, err)
		}
		end, err := event.End.Instant(allDayLocation)
		if err != nil {
			return nil, fmt.Errorf("event %s end: %w", event.ID, err)
		}
		if start.Before(windowEnd) && end.After(windowStart) {
			conflicting = append(conflicting, event.ID)
		}
	}
	return conflicting, nil
}

func meetingInspection(ctx dex.Context) (MeetingRequest, calendar.Event, MeetingOutcome, error) {
	request, err := optionalAttribute(ctx, meetingRequestAttribute)
	if err != nil {
		return MeetingRequest{}, calendar.Event{}, MeetingOutcome{}, err
	}
	hold, err := optionalAttribute(ctx, meetingHoldAttribute)
	if err != nil {
		return MeetingRequest{}, calendar.Event{}, MeetingOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, meetingOutcomeAttribute)
	if err != nil {
		return MeetingRequest{}, calendar.Event{}, MeetingOutcome{}, err
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
// dex:explanation text:"Validate the timed meeting and record it with the picked calendar before calling Google."
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
	return dex.GoTo(sdkgo.StepRef[MeetingRequest](checkMeetingAvailabilityStepType), request), nil
}

func buildMeetingRequest(calendarID string, input Input) (MeetingRequest, error) {
	request := MeetingRequest{
		CalendarID: calendarID, Summary: strings.TrimSpace(input.Summary), Description: strings.TrimSpace(input.Description),
		Start: input.Start, End: input.End, RequestConference: input.RequestConference,
	}
	if request.Summary == "" {
		return MeetingRequest{}, errors.New("summary is required")
	}
	if err := calendar.ValidateEventTimes(input.Start, input.End); err != nil {
		return MeetingRequest{}, err
	}
	if input.Start.IsAllDay() {
		return MeetingRequest{}, errors.New("this example schedules timed meetings; use start.dateTime and end.dateTime")
	}
	if len(input.Attendees) == 0 {
		return MeetingRequest{}, errors.New("attendees must list at least one guest")
	}
	for _, attendee := range input.Attendees {
		address, err := mail.ParseAddress(strings.TrimSpace(attendee))
		if err != nil || address.Name != "" {
			return MeetingRequest{}, fmt.Errorf("attendee %q must be one bare email address", attendee)
		}
		request.Attendees = append(request.Attendees, address.Address)
	}
	return request, nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Complete as busy when free/busy found busy time, otherwise place the hold."
type evaluateMeetingAvailability struct {
	dex.StepDefaultsNoWaitFor[calendar.QueryFreeBusyResult]
}

func (evaluateMeetingAvailability) GetStepType() string { return evaluateMeetingAvailabilityStepType }

func (evaluateMeetingAvailability) Execute(ctx dex.Context, result calendar.QueryFreeBusyResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	var busy []calendar.BusyInterval
	for _, availability := range result.Value.Calendars {
		busy = append(busy, availability.Busy...)
	}
	if len(busy) != 0 {
		outcome := MeetingOutcome{Status: MeetingBusy, CalendarID: request.CalendarID, Busy: busy}
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
	dex.StepDefaultsNoWaitFor[calendar.GetEventResult]
}

func (verifyMeetingHold) GetStepType() string { return verifyMeetingHoldStepType }

func (verifyMeetingHold) Execute(ctx dex.Context, result calendar.GetEventResult) (*dex.StepDecision, error) {
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

func verifyHoldMatchesRequest(hold calendar.Event, request MeetingRequest) error {
	if hold.Status == "cancelled" || hold.IsAllDay {
		return fmt.Errorf("read-back hold %s is cancelled or all-day", hold.ID)
	}
	for _, boundary := range []struct {
		name              string
		stored, requested calendar.EventDateTime
	}{{"start", hold.Start, request.Start}, {"end", hold.End, request.End}} {
		storedInstant, err := boundary.stored.Instant(nil)
		if err != nil {
			return fmt.Errorf("read-back hold %s %s: %w", hold.ID, boundary.name, err)
		}
		requestedInstant, err := boundary.requested.Instant(nil)
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
// dex:explanation text:"Complete as a conflict when another busy event overlaps the hold, otherwise invite."
type evaluateMeetingWindow struct {
	dex.StepDefaultsNoWaitFor[calendar.ListEventsResult]
}

func (evaluateMeetingWindow) GetStepType() string { return evaluateMeetingWindowStepType }

func (evaluateMeetingWindow) Execute(ctx dex.Context, result calendar.ListEventsResult) (*dex.StepDecision, error) {
	request, err := meetingRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	hold, err := meetingHoldAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	allDayLocation := time.UTC
	if result.Value.TimeZone != "" {
		if allDayLocation, err = time.LoadLocation(result.Value.TimeZone); err != nil {
			return dex.ForceFail("calendar time zone is unknown: " + result.Value.TimeZone), nil
		}
	}
	windowStart, err := request.Start.Instant(nil)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	windowEnd, err := request.End.Instant(nil)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	conflicting, err := FindConflictingEventIDs(result.Value.Events, hold.ID, windowStart, windowEnd, allDayLocation)
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
		CalendarID: request.CalendarID, EventID: hold.ID, Attendees: request.Attendees,
	}), nil
}

// dex:group group-id:meeting group-label:"Meeting"
// dex:explanation text:"Record the invited event as the scheduled outcome and complete the Flow."
type completeMeetingBooking struct {
	dex.StepDefaultsNoWaitFor[calendar.UpdateEventResult]
}

func (completeMeetingBooking) GetStepType() string { return completeMeetingBookingStepType }

func (completeMeetingBooking) Execute(ctx dex.Context, result calendar.UpdateEventResult) (*dex.StepDecision, error) {
	event := result.Value.Event
	outcome := MeetingOutcome{Status: MeetingScheduled, CalendarID: event.CalendarID, Event: &event}
	if err := meetingOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
