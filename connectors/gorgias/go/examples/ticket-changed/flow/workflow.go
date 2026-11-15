// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ticketchanged demonstrates the Gorgias ticketChanged Trigger: every ticket event in the
// helpdesk's event log, such as ticket-created or ticket-closed, starts one Flow, which reads the ticket
// back with its customer and latest messages and records its current state.
package ticketchanged

import (
	"errors"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each ticket event starts.
	FlowType = "GorgiasTicketChange"
	// ConnectionName is the static Dex Web connection for Gorgias.
	ConnectionName = "gorgias-ticket-watcher"
	// TicketChangedTriggerBinding is the stable ticketChanged binding that starts this Flow.
	TicketChangedTriggerBinding = "helpdesk-ticket-events"

	recordTicketChangeStepType   = "RecordTicketChange"
	readChangedTicketStepType    = "ReadChangedTicket"
	completeTicketChangeStepType = "CompleteTicketChange"
)

var (
	ticketChangeAttribute        = dex.DefineAttribute[Input]("gorgias-ticket-change")
	ticketChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("gorgias-ticket-change-outcome")
)

// Input is one ticketChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Event is the delivered ticket event.
	Event gorgias.TicketChangedEvent `json:"event"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// TicketID is the changed ticket.
	TicketID int64 `json:"ticketId"`
	// EventType is the delivered Gorgias event type, such as ticket-closed.
	EventType gorgias.TicketEventType `json:"eventType"`
	// Current is the ticket read back from Gorgias with its customer and latest messages.
	Current gorgias.TicketDetails `json:"current"`
}

// Flow records one Gorgias ticket event.
type Flow struct {
	dex.FlowDefaults
	connection gorgias.Connection
}

// NewFlow binds the Gorgias Connection at registration time.
func NewFlow(connection gorgias.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Gorgias connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTicketChange{}),
		dex.DefineStep(gorgias.NewGetTicketStep(gorgias.GetTicketStepConfig[Input]{
			StepType: readChangedTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the ticket back with its customer and latest messages, which the event names only by ID.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(completeTicketChange{}),
		})),
		dex.DefineStep(completeTicketChange{}),
	}
}

// GetConnectorTriggerBindings declares the ticketChanged binding for Dex Web. Studio setup commands cannot
// send a Gorgias API key over HTTP Basic, so the binding has no picker units; its optional eventTypes
// configuration is saved in the project configuration.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		gorgias.DefineTicketChangedTriggerBinding(gorgias.TicketChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: TicketChangedTriggerBinding,
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ticketChangeAttribute, ticketChangeOutcomeAttribute}}
}

// GetDexSummary returns the delivered event and outcome.
//
// dex:field attribute-key:gorgias-ticket-change value-type:json editable:false description:"Delivered ticket event"
// dex:field attribute-key:gorgias-ticket-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gorgias-ticket-change":         change,
		"gorgias-ticket-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered event and the ticket read back from Gorgias.
//
// dex:field attribute-key:gorgias-ticket-change value-type:json editable:false description:"Event ID, type, ticket ID, and user from the event log"
// dex:field attribute-key:gorgias-ticket-change-outcome value-type:json editable:false description:"Current ticket, customer, and latest messages read back from Gorgias"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gorgias-ticket-change":         change,
		"gorgias-ticket-change-outcome": outcome,
	}}, nil
}

func ticketChangeInspection(ctx dex.Context) (Input, ChangeOutcome, error) {
	change, err := optionalAttribute(ctx, ticketChangeAttribute)
	if err != nil {
		return Input{}, ChangeOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, ticketChangeOutcomeAttribute)
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

// ResolveFlowID names one Flow per Gorgias event; a redelivered event finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[gorgias.TicketChangedEvent]) string {
	return "gorgias-ticket-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered event.
func MapToFlowInput(event sdkgo.TriggerEvent[gorgias.TicketChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Event: event.Payload}
}

// AcceptEveryTicketChange admits every event; an application narrows it, for example to rule-made events.
func AcceptEveryTicketChange(sdkgo.TriggerEvent[gorgias.TicketChangedEvent]) bool { return true }

// MapToGetTicketInput reads the ticket the event names.
func MapToGetTicketInput(input Input) gorgias.GetTicketInput {
	return gorgias.GetTicketInput{TicketID: input.Event.TicketID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered ticket event before reading the ticket back."
type recordTicketChange struct {
	dex.StepDefaults
}

func (recordTicketChange) GetStepType() string { return recordTicketChangeStepType }

// WaitFor skips immediately because the Trigger starts the Flow at this Step.
func (recordTicketChange) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordTicketChange) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := validateInput(input); err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := ticketChangeAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedTicketStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the ticket read back from Gorgias as the outcome and complete the Flow."
type completeTicketChange struct {
	dex.StepDefaultsNoWaitFor[gorgias.GetTicketResult]
}

func (completeTicketChange) GetStepType() string { return completeTicketChangeStepType }

func (completeTicketChange) Execute(ctx dex.Context, result gorgias.GetTicketResult) (*dex.StepDecision, error) {
	change, err := ticketChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{TicketID: change.Event.TicketID, EventType: change.Event.EventType, Current: result.Value}
	if err := ticketChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case input.Event.EventID < 1:
		return errors.New("event.eventId is required")
	case input.Event.TicketID < 1:
		return errors.New("event.ticketId is required")
	case !slices.Contains(gorgias.TicketEventTypes(), input.Event.EventType):
		return errors.New("event.eventType must be a ticket event type that ticketChanged reads")
	}
	return nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
