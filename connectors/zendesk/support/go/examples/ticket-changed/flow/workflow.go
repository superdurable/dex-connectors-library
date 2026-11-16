// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ticketchanged demonstrates the Zendesk Support ticketChanged Trigger: every ticket created,
// updated, or deleted in the account starts one Flow, which reads the ticket back with its latest comment
// and records its current state.
package ticketchanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each ticket change starts.
	FlowType = "ZendeskTicketChange"
	// ConnectionName is the static Dex Web connection for Zendesk Support; its user must be an administrator.
	ConnectionName = "zendesk-ticket-watcher"
	// TicketChangedTriggerBinding is the stable ticketChanged binding that starts this Flow.
	TicketChangedTriggerBinding = "zendesk-ticket-changes"

	recordTicketChangeStepType   = "RecordTicketChange"
	readChangedTicketStepType    = "ReadChangedTicket"
	completeTicketChangeStepType = "CompleteTicketChange"
)

var (
	ticketChangeAttribute        = dex.DefineAttribute[Input]("zendesk-ticket-change")
	ticketChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("zendesk-ticket-change-outcome")
)

// Input is one ticketChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change support.TicketChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// TicketID is the changed ticket.
	TicketID int64 `json:"ticketId"`
	// ChangeKind is the delivered change kind.
	ChangeKind support.TicketChangeKind `json:"changeKind"`
	// Current is the ticket read back from Zendesk with its latest comment; nil for a deleted ticket,
	// which is not read.
	Current *support.TicketDetails `json:"current,omitempty"`
}

// Flow records one Zendesk ticket change.
type Flow struct {
	dex.FlowDefaults
	connection support.Connection
}

// NewFlow binds the Zendesk Support Connection at registration time.
func NewFlow(connection support.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zendesk Support connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTicketChange{}),
		dex.DefineStep(support.NewGetTicketStep(support.GetTicketStepConfig[Input]{
			StepType: readChangedTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed ticket back with its latest comment, so the record holds its current state rather than the export snapshot.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(completeTicketChange{}),
		})),
		dex.DefineStep(completeTicketChange{}),
	}
}

// GetConnectorTriggerBindings declares the ticketChanged binding. It has no configuration unit: Zendesk
// has no setup bundle, and the zero configuration delivers created and updated tickets account-wide.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		support.DefineTicketChangedTriggerBinding(support.TicketChangedTriggerBindingConfig{
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

// GetDexSummary returns the change and outcome.
//
// dex:field attribute-key:zendesk-ticket-change value-type:json editable:false description:"Delivered ticket change"
// dex:field attribute-key:zendesk-ticket-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zendesk-ticket-change":         change,
		"zendesk-ticket-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the ticket read back from Zendesk.
//
// dex:field attribute-key:zendesk-ticket-change value-type:json editable:false description:"Change kind and ticket from the export page"
// dex:field attribute-key:zendesk-ticket-change-outcome value-type:json editable:false description:"Current ticket and latest comment read back from Zendesk"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zendesk-ticket-change":         change,
		"zendesk-ticket-change-outcome": outcome,
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
func ResolveFlowID(event sdkgo.TriggerEvent[support.TicketChangedEvent]) string {
	return "zendesk-ticket-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[support.TicketChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryTicketChange admits every change; an application narrows it, for example to one group with
// event.Payload.Ticket.GroupID or to new tickets with TicketChangeCreated.
func AcceptEveryTicketChange(sdkgo.TriggerEvent[support.TicketChangedEvent]) bool { return true }

// MapToGetTicketInput reads the changed ticket with its newest comment.
func MapToGetTicketInput(input Input) support.GetTicketInput {
	return support.GetTicketInput{TicketID: input.Change.Ticket.ID, LatestCommentLimit: 1}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the delivered change, completing at once for a deleted ticket and otherwise reading the ticket back."
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
	if input.Change.ChangeKind == support.TicketChangeDeleted {
		outcome := ChangeOutcome{TicketID: input.Change.Ticket.ID, ChangeKind: input.Change.ChangeKind}
		if err := ticketChangeOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[Input](readChangedTicketStepType), input), nil
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Record the ticket read back from Zendesk as the outcome and complete the Flow."
type completeTicketChange struct {
	dex.StepDefaultsNoWaitFor[support.GetTicketResult]
}

func (completeTicketChange) GetStepType() string { return completeTicketChangeStepType }

func (completeTicketChange) Execute(ctx dex.Context, result support.GetTicketResult) (*dex.StepDecision, error) {
	change, err := ticketChangeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	current := result.Value
	outcome := ChangeOutcome{TicketID: change.Change.Ticket.ID, ChangeKind: change.Change.ChangeKind, Current: &current}
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
		return errors.New("change.ticket.id must be a positive Zendesk ticket ID")
	}
	switch input.Change.ChangeKind {
	case support.TicketChangeCreated, support.TicketChangeUpdated, support.TicketChangeDeleted:
		return nil
	default:
		return errors.New("change.changeKind must be created, updated, or deleted")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
