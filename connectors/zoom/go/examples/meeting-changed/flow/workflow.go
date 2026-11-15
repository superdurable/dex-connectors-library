// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package meetingchanged demonstrates the Zoom meetingChanged Trigger: every meeting the authorized user
// schedules, changes, or that leaves Zoom's scheduled list starts one Flow, which reads a scheduled or
// updated meeting back and records its current state.
package meetingchanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zoom/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each meeting change starts.
	FlowType = "ZoomMeetingChange"
	// ConnectionName is the static Dex Web connection for Zoom.
	ConnectionName = "zoom-meeting-watcher"
	// MeetingChangedTriggerBinding is the stable meetingChanged binding that starts this Flow.
	MeetingChangedTriggerBinding = "host-meeting-changes"

	recordMeetingChangeStepType   = "RecordMeetingChange"
	readChangedMeetingStepType    = "ReadChangedMeeting"
	completeMeetingChangeStepType = "CompleteMeetingChange"
)

var (
	meetingChangeAttribute        = dex.DefineAttribute[Input]("zoom-meeting-change")
	meetingChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("zoom-meeting-change-outcome")
)

// Input is one meetingChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change zoom.MeetingChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// MeetingID is the changed meeting.
	MeetingID int64 `json:"meetingId"`
	// ChangeKind is the delivered change kind.
	ChangeKind zoom.MeetingChangeKind `json:"changeKind"`
	// Current is the meeting read back from Zoom; nil for a removed meeting, or one deleted before the read.
	Current *zoom.Meeting `json:"current,omitempty"`
}

// Flow records one Zoom meeting change.
type Flow struct {
	dex.FlowDefaults
	connection zoom.Connection
}

// NewFlow binds the Zoom Connection at registration time.
func NewFlow(connection zoom.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zoom connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordMeetingChange{}),
		dex.DefineStep(zoom.NewGetMeetingStep(zoom.GetMeetingStepConfig[Input]{
			StepType: readChangedMeetingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed meeting back, so the record holds its full agenda and settings rather than the listed summary.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetMeetingInput,
			Found: sdkgo.GoTo(completeMeetingChange{}),
			// Business reaction: a meeting deleted after the poll listed it is recorded without a current state.
			NotFound: sdkgo.GoTo(completeMeetingChange{}),
		})),
		dex.DefineStep(completeMeetingChange{}),
	}
}

// GetConnectorTriggerBindings declares the meetingChanged binding. It watches the authorized user's own
// meetings, so Dex Web shows no field for it.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		zoom.DefineMeetingChangedTriggerBinding(zoom.MeetingChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: MeetingChangedTriggerBinding,
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{meetingChangeAttribute, meetingChangeOutcomeAttribute}}
}

// GetDexSummary returns the change and outcome.
//
// dex:field attribute-key:zoom-meeting-change value-type:json editable:false description:"Delivered meeting change"
// dex:field attribute-key:zoom-meeting-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := meetingChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoom-meeting-change":         change,
		"zoom-meeting-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the meeting read back from Zoom.
//
// dex:field attribute-key:zoom-meeting-change value-type:json editable:false description:"Change kind, listed meeting, and previous listed state"
// dex:field attribute-key:zoom-meeting-change-outcome value-type:json editable:false description:"Current meeting read back from Zoom"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := meetingChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoom-meeting-change":         change,
		"zoom-meeting-change-outcome": outcome,
	}}, nil
}

func meetingChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, meetingChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, meetingChangeOutcomeAttribute)
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
func ResolveFlowID(event sdkgo.TriggerEvent[zoom.MeetingChangedEvent]) string {
	return "zoom-meeting-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[zoom.MeetingChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryMeetingChange admits every change; an application narrows it, for example to reschedules.
func AcceptEveryMeetingChange(sdkgo.TriggerEvent[zoom.MeetingChangedEvent]) bool { return true }

// MapToGetMeetingInput reads the changed meeting by its ID.
func MapToGetMeetingInput(input Input) zoom.GetMeetingInput {
	return zoom.GetMeetingInput{MeetingID: input.Change.Meeting.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered change, completing at once for a removed meeting and otherwise reading it back."
type recordMeetingChange struct {
	dex.StepDefaults
}

func (recordMeetingChange) GetStepType() string { return recordMeetingChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordMeetingChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordMeetingChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := meetingChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	if input.Change.ChangeKind == zoom.MeetingChangeRemoved {
		outcome := ChangeOutcome{MeetingID: input.Change.Meeting.ID, ChangeKind: input.Change.ChangeKind}
		if err := meetingChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedMeetingStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the meeting read back from Zoom, or none when it was deleted before the read, and complete the Flow."
type completeMeetingChange struct {
	dex.StepDefaultsNoWaitFor[zoom.GetMeetingResult]
}

func (completeMeetingChange) GetStepType() string { return completeMeetingChangeStepType }

func (completeMeetingChange) Execute(ctx dex.Context, result zoom.GetMeetingResult) (*dex.StepDecision, error) {
	change, err := meetingChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{MeetingID: change.Change.Meeting.ID, ChangeKind: change.Change.ChangeKind}
	if result.Branch == zoom.GetMeetingBranchFound {
		current := result.Value
		outcome.Current = &current
	}
	if err := meetingChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case input.Change.Meeting.ID < 1:
		return errors.New("change.meeting.id is required")
	}
	switch input.Change.ChangeKind {
	case zoom.MeetingChangeScheduled, zoom.MeetingChangeUpdated, zoom.MeetingChangeRemoved:
		return nil
	default:
		return errors.New("change.changeKind must be scheduled, updated, or removed")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
