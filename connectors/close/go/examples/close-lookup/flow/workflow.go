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
const tasksStepType = "ReadCloseTasks"
const notesStepType = "ReadCloseNotes"
const callsStepType = "ReadCloseCalls"
const emailsStepType = "ReadCloseEmails"
const smsStepType = "ReadCloseSMS"

var selectedLeadAttribute = dex.DefineAttribute[close.Lead]("close-selected-lead")
var selectedContactsAttribute = dex.DefineAttribute[[]close.Contact]("close-selected-contacts")
var selectedOpportunitiesAttribute = dex.DefineAttribute[[]close.Opportunity]("close-selected-opportunities")
var selectedTasksAttribute = dex.DefineAttribute[[]close.Task]("close-selected-tasks")
var selectedNotesAttribute = dex.DefineAttribute[[]close.Note]("close-selected-notes")
var selectedCallsAttribute = dex.DefineAttribute[[]close.CallActivity]("close-selected-calls")
var selectedEmailsAttribute = dex.DefineAttribute[[]close.EmailActivity]("close-selected-emails")

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
	// Tasks contains the first page of lead tasks.
	Tasks []close.Task `json:"tasks"`
	// Notes contains the first page of note activities.
	Notes []close.Note `json:"notes"`
	// Calls contains the first page of logged call activities.
	Calls []close.CallActivity `json:"calls"`
	// Emails contains the first page of email activities.
	Emails []close.EmailActivity `json:"emails"`
	// Messages contains the first page of SMS activities.
	Messages []close.SMSActivity `json:"messages"`
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{selectedLeadAttribute, selectedContactsAttribute, selectedOpportunitiesAttribute, selectedTasksAttribute, selectedNotesAttribute, selectedCallsAttribute, selectedEmailsAttribute}}
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
		dex.DefineStep(close.NewListOpportunitiesStep(close.ListOpportunitiesStepConfig[close.ListOpportunitiesInput]{StepType: opportunitiesStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read opportunities belonging to the selected lead."}, MapToOperationInput: func(input close.ListOpportunitiesInput) close.ListOpportunitiesInput { return input }, Listed: sdkgo.GoTo(afterOpportunities{})})),
		dex.DefineStep(afterOpportunities{}),
		dex.DefineStep(close.NewListTasksStep(close.ListTasksStepConfig[close.ListTasksInput]{StepType: tasksStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read tasks belonging to the selected lead."}, MapToOperationInput: func(input close.ListTasksInput) close.ListTasksInput { return input }, Listed: sdkgo.GoTo(afterTasks{})})),
		dex.DefineStep(afterTasks{}),
		dex.DefineStep(close.NewListNotesStep(close.ListNotesStepConfig[close.ListNotesInput]{StepType: notesStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read notes belonging to the selected lead."}, MapToOperationInput: func(input close.ListNotesInput) close.ListNotesInput { return input }, Listed: sdkgo.GoTo(afterNotes{})})),
		dex.DefineStep(afterNotes{}),
		dex.DefineStep(close.NewListCallsStep(close.ListCallsStepConfig[close.ListCallsInput]{StepType: callsStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read calls belonging to the selected lead."}, MapToOperationInput: func(input close.ListCallsInput) close.ListCallsInput { return input }, Listed: sdkgo.GoTo(afterCalls{})})),
		dex.DefineStep(afterCalls{}),
		dex.DefineStep(close.NewListEmailsStep(close.ListEmailsStepConfig[close.ListEmailsInput]{StepType: emailsStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read emails belonging to the selected lead."}, MapToOperationInput: func(input close.ListEmailsInput) close.ListEmailsInput { return input }, Listed: sdkgo.GoTo(afterEmails{})})),
		dex.DefineStep(afterEmails{}),
		dex.DefineStep(close.NewListSMSStep(close.ListSMSStepConfig[close.ListSMSInput]{StepType: smsStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read SMS belonging to the selected lead."}, MapToOperationInput: func(input close.ListSMSInput) close.ListSMSInput { return input }, Listed: sdkgo.GoTo(completeLookup{})})),
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
// dex:explanation text:"Save opportunities and request the lead's tasks."
type afterOpportunities struct {
	dex.StepDefaultsNoWaitFor[close.ListOpportunitiesResult]
}

func (afterOpportunities) Execute(ctx dex.Context, result close.ListOpportunitiesResult) (*dex.StepDecision, error) {
	if err := selectedOpportunitiesAttribute.Set(ctx, result.Value.Opportunities); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListTasksInput](tasksStepType), close.ListTasksInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save tasks and request the lead's notes."
type afterTasks struct {
	dex.StepDefaultsNoWaitFor[close.ListTasksResult]
}

func (afterTasks) Execute(ctx dex.Context, result close.ListTasksResult) (*dex.StepDecision, error) {
	if err := selectedTasksAttribute.Set(ctx, result.Value.Tasks); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListNotesInput](notesStepType), close.ListNotesInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save notes and request the lead's call activities."
type afterNotes struct {
	dex.StepDefaultsNoWaitFor[close.ListNotesResult]
}

func (afterNotes) Execute(ctx dex.Context, result close.ListNotesResult) (*dex.StepDecision, error) {
	if err := selectedNotesAttribute.Set(ctx, result.Value.Notes); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListCallsInput](callsStepType), close.ListCallsInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save calls and request the lead's email activities."
type afterCalls struct {
	dex.StepDefaultsNoWaitFor[close.ListCallsResult]
}

func (afterCalls) Execute(ctx dex.Context, result close.ListCallsResult) (*dex.StepDecision, error) {
	if err := selectedCallsAttribute.Set(ctx, result.Value.Calls); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListEmailsInput](emailsStepType), close.ListEmailsInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Save emails and request the lead's SMS activities."
type afterEmails struct {
	dex.StepDefaultsNoWaitFor[close.ListEmailsResult]
}

func (afterEmails) Execute(ctx dex.Context, result close.ListEmailsResult) (*dex.StepDecision, error) {
	if err := selectedEmailsAttribute.Set(ctx, result.Value.Emails); err != nil {
		return nil, err
	}
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[close.ListSMSInput](smsStepType), close.ListSMSInput{LeadID: lead.ID, Limit: 10}), nil
}

// dex:group group-id:close-lookup group-label:"Close lookup"
// dex:explanation text:"Complete the Flow with the lead and related records."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[close.ListSMSResult]
}

func (completeLookup) GetStepType() string { return "CompleteCloseLookup" }
func (completeLookup) Execute(ctx dex.Context, result close.ListSMSResult) (*dex.StepDecision, error) {
	lead, err := selectedLeadAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	contacts, err := selectedContactsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	opportunities, err := selectedOpportunitiesAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	tasks, err := selectedTasksAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	notes, err := selectedNotesAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	calls, err := selectedCallsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	emails, err := selectedEmailsAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GracefulComplete(Outcome{Lead: lead, Contacts: contacts, Opportunities: opportunities, Tasks: tasks, Notes: notes, Calls: calls, Emails: emails, Messages: result.Value.Messages}), nil
}
