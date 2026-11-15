// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package ticketchanged demonstrates the Zoho Desk ticketChanged Trigger: every ticket created or changed
// in the watched department starts one Flow, which reads the ticket back with its newest threads and
// comments and records its current state.
package ticketchanged

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity each ticket change starts.
	FlowType = "ZohoDeskTicketChange"
	// ConnectionName is the static Dex Web connection for Zoho Desk.
	ConnectionName = "zoho-desk-ticket-watcher"
	// TicketChangedTriggerBinding is the stable ticketChanged binding that starts this Flow.
	TicketChangedTriggerBinding = "desk-ticket-changes"

	recordTicketChangeStepType   = "RecordTicketChange"
	readChangedTicketStepType    = "ReadChangedTicket"
	completeTicketChangeStepType = "CompleteTicketChange"
)

var (
	ticketChangeAttribute        = dex.DefineAttribute[Input]("zoho-desk-ticket-change")
	ticketChangeOutcomeAttribute = dex.DefineAttribute[ChangeOutcome]("zoho-desk-ticket-change-outcome")
)

// Input is one ticketChanged delivery.
type Input struct {
	// TriggerEventID is the stable Trigger event ID that also names the Flow.
	TriggerEventID string `json:"triggerEventId"`
	// Change is the delivered change.
	Change desk.TicketChangedEvent `json:"change"`
}

// ChangeOutcome is the Flow result and the value of its outcome Attribute.
type ChangeOutcome struct {
	// TicketID is the changed ticket.
	TicketID string `json:"ticketId"`
	// ChangeKind is the delivered change kind.
	ChangeKind desk.TicketChangeKind `json:"changeKind"`
	// Current is the ticket read back from Zoho Desk with its newest threads and comments.
	Current desk.TicketDetails `json:"current"`
}

// Flow records one Zoho Desk ticket change.
type Flow struct {
	dex.FlowDefaults
	connection desk.Connection
}

// NewFlow binds the Zoho Desk Connection at registration time.
func NewFlow(connection desk.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zoho Desk connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTicketChange{}),
		dex.DefineStep(desk.NewGetTicketStep(desk.GetTicketStepConfig[Input]{
			StepType: readChangedTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "change", GroupLabel: "Change",
				Explanation: "Read the changed ticket back with its newest threads and comments, which the search page leaves out.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(completeTicketChange{}),
		})),
		dex.DefineStep(completeTicketChange{}),
	}
}

// GetConnectorTriggerBindings declares the ticketChanged binding and its department picker for Dex Web.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		desk.DefineTicketChangedTriggerBinding(desk.TicketChangedTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: TicketChangedTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "watchedDepartment", UnitID: desk.UIUnitDepartmentPicker, Label: "Watched department",
				Description: "Choose the Zoho Desk department whose created and changed tickets start this Flow; the list comes from the connection's organization and saves the department ID and name. Leave it blank to watch every department the connection's agent can see. The first start records recent tickets as a baseline and starts no Flow for them. Restart the Worker after saving; a changed department records a new baseline.",
				Bindings: []sdkgo.ConnectorUIBinding{
					{Port: desk.UIDepartmentPickerPortDepartmentID, JSONPointer: "/departmentId"},
					{Port: desk.UIDepartmentPickerPortDepartmentName, JSONPointer: "/departmentName"},
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{ticketChangeAttribute, ticketChangeOutcomeAttribute}}
}

// GetDexSummary returns the change kind and outcome.
//
// dex:field attribute-key:zoho-desk-ticket-change value-type:json editable:false description:"Delivered ticket change"
// dex:field attribute-key:zoho-desk-ticket-change-outcome value-type:json editable:false description:"Recorded change"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoho-desk-ticket-change":         change,
		"zoho-desk-ticket-change-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the delivered change and the ticket read back from Zoho Desk.
//
// dex:field attribute-key:zoho-desk-ticket-change value-type:json editable:false description:"Organization, change kind, and ticket from the search page"
// dex:field attribute-key:zoho-desk-ticket-change-outcome value-type:json editable:false description:"Current ticket, threads, and comments read back from Zoho Desk"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	change, outcome, err := ticketChangeInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoho-desk-ticket-change":         change,
		"zoho-desk-ticket-change-outcome": outcome,
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
func ResolveFlowID(event sdkgo.TriggerEvent[desk.TicketChangedEvent]) string {
	return "zoho-desk-ticket-change-" + event.ID
}

// MapToFlowInput keeps the Trigger event ID beside the delivered change.
func MapToFlowInput(event sdkgo.TriggerEvent[desk.TicketChangedEvent]) Input {
	return Input{TriggerEventID: event.ID, Change: event.Payload}
}

// AcceptEveryTicketChange admits every change; an application narrows it, for example to new tickets.
func AcceptEveryTicketChange(sdkgo.TriggerEvent[desk.TicketChangedEvent]) bool { return true }

// MapToGetTicketInput reads the changed ticket with the default thread and comment limits.
func MapToGetTicketInput(input Input) desk.GetTicketInput {
	return desk.GetTicketInput{TicketID: input.Change.Ticket.ID}
}

// dex:group group-id:change group-label:"Change"
// dex:explanation text:"Validate and record the delivered ticket change, then read the ticket back."
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
// dex:explanation text:"Record the ticket read back from Zoho Desk as the outcome and complete the Flow."
type completeTicketChange struct {
	dex.StepDefaultsNoWaitFor[desk.GetTicketResult]
}

func (completeTicketChange) GetStepType() string { return completeTicketChangeStepType }

func (completeTicketChange) Execute(ctx dex.Context, result desk.GetTicketResult) (*dex.StepDecision, error) {
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
	case strings.TrimSpace(input.Change.OrgID) == "" || strings.TrimSpace(input.Change.Ticket.ID) == "":
		return errors.New("change.orgId and change.ticket.id are required")
	}
	switch input.Change.ChangeKind {
	case desk.TicketChangeCreated, desk.TicketChangeUpdated:
		return nil
	default:
		return errors.New("change.changeKind must be created or updated")
	}
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
