// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ticketchanged demonstrates the Freshdesk ticketChanged Trigger: every ticket created or updated
// in the helpdesk starts one Flow, which reads the ticket back with its requester and first conversations
// and records its current state.
package ticketchanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each ticket change starts.
	FlowType = "FreshdeskTicketChange"
	// ConnectionName is the static Dex Web connection for Freshdesk.
	ConnectionName = "freshdesk-ticket-watcher"
	// TicketChangedTriggerBinding is the stable ticketChanged binding that starts this Flow.
	TicketChangedTriggerBinding = "helpdesk-ticket-changes"

	recordTicketChangeStepType   = "RecordTicketChange"
	readChangedTicketStepType    = "ReadChangedTicket"
	completeTicketChangeStepType = "CompleteTicketChange"
)

var (
	ticketChangeAttribute        = dex.DefineAttribute[Input]("freshdesk-ticket-change")
	ticketChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("freshdesk-ticket-change-outcome")
)

// Input is one ticketChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change freshdesk.TicketChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// TicketID is the changed ticket.
	TicketID int64 `json:"ticketId"`
	// ChangeKind is the delivered change kind.
	ChangeKind freshdesk.TicketChangeKind `json:"changeKind"`
	// Current is the ticket read back from Freshdesk with its requester and first conversations.
	Current freshdesk.TicketDetails `json:"current"`
}

// Flow records one Freshdesk ticket change.
type Flow struct {
	dex.FlowDefaults
	connection freshdesk.Connection
}

// NewFlow binds the Freshdesk Connection at registration time.
func NewFlow(connection freshdesk.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Freshdesk connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTicketChange{}),
		dex.DefineStep(freshdesk.NewGetTicketStep(freshdesk.GetTicketStepConfig[Input]{
			StepType: readChangedTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed ticket back with its requester and first conversations, which the ticket list omits.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(completeTicketChange{}),
		})),
		dex.DefineStep(completeTicketChange{}),
	}
}

// GetConnectorTriggerBindings declares the ticketChanged binding for Dex Web. Freshdesk setup commands
// cannot send the agent's HTTP Basic API key, so the binding has no picker units; its optional
// changeKinds configuration is saved in the project configuration.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		freshdesk.DefineTicketChangedTriggerBinding(freshdesk.TicketChangedTriggerBindingConfig{
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

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:freshdesk-ticket-change value-type:json editable:false description:"Delivered ticket change"
// dex:field attribute-key:freshdesk-ticket-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"freshdesk-ticket-change":         change,
		"freshdesk-ticket-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the ticket read back from Freshdesk.
//
// dex:field attribute-key:freshdesk-ticket-change value-type:json editable:false description:"Change kind and ticket from the ticket list"
// dex:field attribute-key:freshdesk-ticket-change-outcome value-type:json editable:false description:"Current ticket, requester, and conversations read back from Freshdesk"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"freshdesk-ticket-change":         change,
		"freshdesk-ticket-change-outcome": outcome,
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

// ResolveFlowID names one Flow per ticket version; a redelivered change finds its existing Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]) string {
	return "freshdesk-ticket-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryTicketChange admits every change; an application narrows it, for example to urgent tickets.
func AcceptEveryTicketChange(sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]) bool { return true }

// MapToGetTicketInput reads the changed ticket.
func MapToGetTicketInput(input Input) freshdesk.GetTicketInput {
	return freshdesk.GetTicketInput{TicketID: input.Change.Ticket.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered ticket change before reading the ticket back."
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
// dex:explanation text:"Record the ticket read back from Freshdesk as the outcome and complete the Flow."
type completeTicketChange struct {
	dex.StepDefaultsNoWaitFor[freshdesk.GetTicketResult]
}

func (completeTicketChange) GetStepType() string { return completeTicketChangeStepType }

func (completeTicketChange) Execute(ctx dex.Context, result freshdesk.GetTicketResult) (*dex.StepDecision, error) {
	change, err := ticketChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := ChangeOutcome{TicketID: change.Change.Ticket.ID, ChangeKind: change.Change.ChangeKind, Current: result.Value}
	if err := ticketChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func validateInput(input Input) error {
	switch {
	case strings.TrimSpace(input.TriggerEventID) == "":
		return errors.New("triggerEventId is required")
	case input.Change.Ticket.ID < 1:
		return errors.New("change.ticket.id is required")
	}
	switch input.Change.ChangeKind {
	case freshdesk.TicketChangeCreated, freshdesk.TicketChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
