// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package eventchanged demonstrates the Google Calendar eventChanged Trigger: every event created,
// updated, or cancelled on the picked calendar starts one Flow, which reads the event back and records
// its current state.
package eventchanged

import (
	"errors"
	"strings"

	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each calendar change starts.
	FlowType = "GoogleCalendarEventChange"
	// ConnectionName is the static Dex Web connection for Google Calendar.
	ConnectionName = "google-calendar-watcher"
	// EventChangedTriggerBinding is the stable eventChanged binding that starts this Flow.
	EventChangedTriggerBinding = "calendar-event-changes"

	recordEventChangeStepType   = "RecordEventChange"
	readChangedEventStepType    = "ReadChangedEvent"
	completeEventChangeStepType = "CompleteEventChange"
)

var (
	eventChangeAttribute        = dex.DefineAttribute[Input]("google-calendar-event-change")
	eventChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("google-calendar-event-change-outcome")
)

// Input is one eventChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change calendar.EventChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// CalendarID is the watched calendar.
	CalendarID string `json:"calendarId"`
	// EventID is the changed event.
	EventID string `json:"eventId"`
	// ChangeKind is the delivered change kind.
	ChangeKind calendar.EventChangeKind `json:"changeKind"`
	// Current is the event read back from Google Calendar; nil for a cancelled event, which is not read.
	Current *calendar.Event `json:"current,omitempty"`
}

// Flow records one Google Calendar event change.
type Flow struct {
	dex.FlowDefaults
	connection calendar.Connection
}

// NewFlow binds the Google Calendar Connection at registration time.
func NewFlow(connection calendar.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Google Calendar connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordEventChange{}),
		dex.DefineStep(calendar.NewGetEventStep(calendar.GetEventStepConfig[Input]{
			StepType: readChangedEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed event back, so the record holds its current state rather than the sync snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetEventInput,
			Found: sdkgo.GoTo(completeEventChange{}),
		})),
		dex.DefineStep(completeEventChange{}),
	}
}

// GetConnectorTriggerBindings declares the eventChanged binding and its calendar picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		calendar.DefineEventChangedTriggerBinding(calendar.EventChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: EventChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedCalendar", UnitID: calendar.UIUnitCalendarPicker, Label: "Watched calendar",
				Description: "Choose the calendar whose created, updated, and cancelled events start this Flow; the list comes from this connection's Google Calendar list and saves the calendar ID and name. Leave it unsaved to watch the authorized account's primary calendar. The first start records existing events as a baseline and starts no Flow for them. Restart the Worker after saving; a changed calendar records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: calendar.UICalendarPickerPortCalendarID, JSONPointer: "/calendarId"},
					{Port: calendar.UICalendarPickerPortCalendarName, JSONPointer: "/calendarName"},
				},
			}}},
		}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the change and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{eventChangeAttribute, eventChangeOutcomeAttribute}}
}

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:google-calendar-event-change value-type:json editable:false description:"Delivered calendar change"
// dex:field attribute-key:google-calendar-event-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := eventChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-calendar-event-change":         change,
		"google-calendar-event-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the event read back from Google Calendar.
//
// dex:field attribute-key:google-calendar-event-change value-type:json editable:false description:"Calendar, change kind, and event from the sync page"
// dex:field attribute-key:google-calendar-event-change-outcome value-type:json editable:false description:"Current event read back from Google Calendar"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := eventChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-calendar-event-change":         change,
		"google-calendar-event-change-outcome": outcome,
	}}, nil
}

func eventChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, eventChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, eventChangeOutcomeAttribute)
	return change, outcome, err
}

// optionalAttribute returns the zero value while a running Flow has not set the Attribute yet.
func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// ResolveFlowID names one Flow per change; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[calendar.EventChangedEvent]) string {
	return "google-calendar-event-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[calendar.EventChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryEventChange admits every change; an application narrows it, for example to cancellations.
func AcceptEveryEventChange(sdkgo.TriggerEvent[calendar.EventChangedEvent]) bool { return true }

// MapToGetEventInput reads the changed event from its calendar.
func MapToGetEventInput(input Input) calendar.GetEventInput {
	return calendar.GetEventInput{CalendarID: input.Change.CalendarID, EventID: input.Change.Event.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered change, completing at once for a cancellation and otherwise reading the event back."
type recordEventChange struct {
	dex.StepDefaults
}

func (recordEventChange) GetStepType() string { return recordEventChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordEventChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordEventChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := eventChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Change.ChangeKind == calendar.EventChangeCancelled {
		outcome := ChangeOutcome{CalendarID: input.Change.CalendarID, EventID: input.Change.Event.ID, ChangeKind: input.Change.ChangeKind}
		if err := eventChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedEventStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the event read back from Google Calendar as the outcome and complete the Flow."
type completeEventChange struct {
	dex.StepDefaultsNoWaitFor[calendar.GetEventResult]
}

func (completeEventChange) GetStepType() string { return completeEventChangeStepType }

func (completeEventChange) Execute(ctx dex.Context, result calendar.GetEventResult) (*dex.StepDecision, error) {
	change, err := eventChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome := ChangeOutcome{
		CalendarID: change.Change.CalendarID, EventID: change.Change.Event.ID, ChangeKind: change.Change.ChangeKind, Current: &current,
	}
	if err := eventChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case strings.TrimSpace(input.Change.CalendarID) == "" || strings.TrimSpace(input.Change.Event.ID) == "":
		return errors.New("change.calendarId and change.event.id are required")
	}
	switch input.Change.ChangeKind {
	case calendar.EventChangeCreated, calendar.EventChangeUpdated, calendar.EventChangeCancelled:
		return nil
	default:
		return errors.New("change.changeKind must be created, updated, or cancelled")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
