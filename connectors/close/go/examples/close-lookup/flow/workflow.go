// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package closelookup demonstrates Close reads and contact creation in a Dex Flow.
package closelookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/close/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "CloseLookup"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "close"
const firstStepType = "ReadCloseFirst"
const secondStepType = "ReadCloseSecond"
const contactsStepType = "ReadCloseContacts"
const createContactStepType = "CreateCloseContact"
const opportunitiesStepType = "ReadCloseOpportunities"

var selectedLeadAttribute = dex.DefineAttribute[close.Lead]("close-selected-lead")
var selectedContactsAttribute = dex.DefineAttribute[[]close.Contact]("close-selected-contacts")

// Request is the typed Start Flow input.
type Request struct {
	// Skip is the zero-based lead offset.
	Skip int `json:"skip,omitempty"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Lead contains the provider data returned by the Flow.
	Lead close.Lead `json:"lead"`
	// Contacts contains the first page of people on the lead.
	Contacts []close.Contact `json:"contacts"`
	// Opportunities contains the first page of deals on the selected lead.
	Opportunities []close.Opportunity `json:"opportunities"`
}

// Flow reads the first and second provider resources.
type Flow struct {
	dex.FlowDefaults
	connection close.Connection
}

// NewFlow binds the static connection at registration.
func NewFlow(connection close.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema declares the example's durable state.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{selectedLeadAttribute, selectedContactsAttribute}}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the example's read-only Dex Web summary.
func (*Flow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay returns the example's read-only Dex Web display.
func (*Flow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(close.NewListLeadsStep(close.ListLeadsStepConfig[Request]{StepType: firstStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read Close records."}, MapToOperationInput: MapToListLeadsInput, Listed: sdkgo.GoTo(afterFirst{})})),
		dex.DefineStep(afterFirst{}),
		dex.DefineStep(close.NewGetLeadStep(close.GetLeadStepConfig[close.GetLeadInput]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read the related Close resource."}, MapToOperationInput: MapToGetLeadInput, Found: sdkgo.GoTo(afterLead{})})),
		dex.DefineStep(afterLead{}),
		dex.DefineStep(close.NewCreateContactStep(close.CreateContactStepConfig[close.CreateContactInput]{StepType: createContactStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Create a sample contact on the selected lead."}, MapToOperationInput: func(input close.CreateContactInput) close.CreateContactInput { return input }, Created: sdkgo.GoTo(afterContactCreated{})})),
		dex.DefineStep(afterContactCreated{}),
		dex.DefineStep(close.NewListContactsStep(close.ListContactsStepConfig[close.ListContactsInput]{StepType: contactsStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read contacts belonging to the selected lead."}, MapToOperationInput: func(input close.ListContactsInput) close.ListContactsInput { return input }, Listed: sdkgo.GoTo(afterContacts{})})),
		dex.DefineStep(afterContacts{}),
		dex.DefineStep(close.NewListOpportunitiesStep(close.ListOpportunitiesStepConfig[close.ListOpportunitiesInput]{StepType: opportunitiesStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read opportunities belonging to the selected lead."}, MapToOperationInput: func(input close.ListOpportunitiesInput) close.ListOpportunitiesInput { return input }, Listed: sdkgo.GoTo(completeLookup{})})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToListLeadsInput maps the start input to the first read.
func MapToListLeadsInput(request Request) close.ListLeadsInput {
	return close.ListLeadsInput{Skip: request.Skip, Limit: 10}
}

// MapToGetLeadInput maps the transition value to the second read.
func MapToGetLeadInput(input close.GetLeadInput) close.GetLeadInput { return input }

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Start the Close read with the typed request."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCloseLookup" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](firstStepType), request), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Choose the related Close read."
type afterFirst struct {
	dex.StepDefaultsNoWaitFor[close.ListLeadsResult]
}

func (afterFirst) GetStepType() string { return "AfterCloseFirst" }
func (afterFirst) Execute(_ dex.Context, result close.ListLeadsResult) (*dex.StepDecision, error) {
	if len(result.Value.Leads) == 0 {
		return dex.GracefulComplete(Outcome{}), nil
	}
	return dex.GoTo(sdkgo.StepRef[close.GetLeadInput](secondStepType), close.GetLeadInput{LeadID: result.Value.Leads[0].ID}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save the selected lead and request its contacts."
type afterLead struct {
	dex.StepDefaultsNoWaitFor[close.GetLeadResult]
}

func (afterLead) Execute(ctx dex.Context, result close.GetLeadResult) (*dex.StepDecision, error) {
	if err := selectedLeadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.CreateContactInput](createContactStepType), close.CreateContactInput{LeadID: result.Value.ID, Name: "Ada Example"}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Read the contact page after Close confirms creation."
type afterContactCreated struct {
	dex.StepDefaultsNoWaitFor[close.CreateContactResult]
}

func (afterContactCreated) Execute(_ dex.Context, result close.CreateContactResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[close.ListContactsInput](contactsStepType), close.ListContactsInput{LeadID: result.Value.LeadID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save the contacts and request the lead's opportunities."
type afterContacts struct {
	dex.StepDefaultsNoWaitFor[close.ListContactsResult]
}

func (afterContacts) Execute(ctx dex.Context, result close.ListContactsResult) (*dex.StepDecision, error) {
	if err := selectedContactsAttribute.Set(ctx, result.Value.Contacts); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListOpportunitiesInput](opportunitiesStepType), close.ListOpportunitiesInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Complete the Flow with the lead, contacts, and opportunities."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[close.ListOpportunitiesResult]
}

func (completeLookup) GetStepType() string { return "CompleteCloseLookup" }
func (completeLookup) Execute(ctx dex.Context, result close.ListOpportunitiesResult) (*dex.StepDecision, error) {
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contacts, err := selectedContactsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Outcome{Lead: lead, Contacts: contacts, Opportunities: result.Value.Opportunities}), nil
}
