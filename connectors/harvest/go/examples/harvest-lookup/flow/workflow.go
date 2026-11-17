// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package harvestlookup demonstrates three Harvest reads in a Dex Flow.
package harvestlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/harvest/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "HarvestLookup"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "harvest"
const firstStepType = "ReadHarvestFirst"
const projectStepType = "ReadHarvestProject"
const secondStepType = "ReadHarvestSecond"
const timeEntryStepType = "ReadHarvestTimeEntry"
const currentUserStepType = "ReadHarvestCurrentUser"
const clientStepType = "ReadHarvestClients"
const contactStepType = "ReadHarvestContacts"
const companyStepType = "ReadHarvestCompany"

// Request is the typed Start Flow input.
type Request struct {
	// PerPage caps the projects returned in the first page.
	PerPage int `json:"perPage,omitempty"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Company contains the Harvest account settings read by the Flow.
	Company harvest.CompanyRecord `json:"company"`
}

// Flow reads the first and second provider resources.
type Flow struct {
	dex.FlowDefaults
	connection harvest.Connection
}

// NewFlow binds the static connection at registration.
func NewFlow(connection harvest.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema declares the example's durable state.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

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
		dex.DefineStep(harvest.NewListProjectsStep(harvest.ListProjectsStepConfig[Request]{StepType: firstStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read Harvest records."}, MapToOperationInput: MapToListProjectsInput, Listed: sdkgo.GoTo(afterFirst{})})),
		dex.DefineStep(afterFirst{}),
		dex.DefineStep(harvest.NewGetProjectStep(harvest.GetProjectStepConfig[harvest.ListProjectsResult]{StepType: projectStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read the first listed Harvest project."}, MapToOperationInput: MapToGetProjectInput, Found: sdkgo.GoTo(afterProject{})})),
		dex.DefineStep(afterProject{}),
		dex.DefineStep(harvest.NewListTimeEntriesStep(harvest.ListTimeEntriesStepConfig[harvest.GetProjectResult]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read the related Harvest resource."}, MapToOperationInput: MapToListTimeEntriesInput, Listed: sdkgo.GoTo(afterTimeEntries{})})),
		dex.DefineStep(afterTimeEntries{}),
		dex.DefineStep(harvest.NewGetTimeEntryStep(harvest.GetTimeEntryStepConfig[harvest.ListTimeEntriesResult]{StepType: timeEntryStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read the first listed time entry."}, MapToOperationInput: MapToGetTimeEntryInput, Found: sdkgo.GoTo(afterTimeEntry{})})),
		dex.DefineStep(afterTimeEntry{}),
		dex.DefineStep(harvest.NewGetCurrentUserStep(harvest.GetCurrentUserStepConfig[harvest.GetTimeEntryResult]{StepType: currentUserStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read the connection owner's Harvest user."}, MapToOperationInput: MapToGetCurrentUserInput, Found: sdkgo.GoTo(afterCurrentUser{})})),
		dex.DefineStep(afterCurrentUser{}),
		dex.DefineStep(harvest.NewListClientsStep(harvest.ListClientsStepConfig[harvest.GetCurrentUserResult]{StepType: clientStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read Harvest clients."}, MapToOperationInput: MapToListClientsInput, Listed: sdkgo.GoTo(afterClients{})})),
		dex.DefineStep(afterClients{}),
		dex.DefineStep(harvest.NewListContactsStep(harvest.ListContactsStepConfig[harvest.ListClientsResult]{StepType: contactStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read contacts for the first client."}, MapToOperationInput: MapToListContactsInput, Listed: sdkgo.GoTo(afterContacts{})})),
		dex.DefineStep(afterContacts{}),
		dex.DefineStep(harvest.NewGetCompanyStep(harvest.GetCompanyStepConfig[harvest.ListContactsResult]{StepType: companyStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read company settings."}, MapToOperationInput: MapToGetCompanyInput, Found: sdkgo.GoTo(completeLookup{})})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToListProjectsInput maps the start input to the first read.
func MapToListProjectsInput(request Request) harvest.ListProjectsInput {
	return harvest.ListProjectsInput{PerPage: request.PerPage}
}

// MapToGetProjectInput selects the first listed project for a detail read.
func MapToGetProjectInput(input harvest.ListProjectsResult) harvest.GetProjectInput {
	if len(input.Value.Projects) == 0 {
		return harvest.GetProjectInput{}
	}
	return harvest.GetProjectInput{ID: input.Value.Projects[0].ID}
}

// MapToListTimeEntriesInput maps the transition value to the time read.
func MapToListTimeEntriesInput(_ harvest.GetProjectResult) harvest.ListTimeEntriesInput {
	return harvest.ListTimeEntriesInput{PerPage: 10}
}

// MapToGetTimeEntryInput selects the first time entry for a detail read.
func MapToGetTimeEntryInput(input harvest.ListTimeEntriesResult) harvest.GetTimeEntryInput {
	if len(input.Value.TimeEntries) == 0 {
		return harvest.GetTimeEntryInput{}
	}
	return harvest.GetTimeEntryInput{ID: input.Value.TimeEntries[0].ID}
}

// MapToGetCurrentUserInput selects the connection owner after the time read.
func MapToGetCurrentUserInput(_ harvest.GetTimeEntryResult) harvest.GetCurrentUserInput {
	return harvest.GetCurrentUserInput{}
}

// MapToListClientsInput selects a bounded client page after the user read.
func MapToListClientsInput(_ harvest.GetCurrentUserResult) harvest.ListClientsInput {
	return harvest.ListClientsInput{PerPage: 10}
}

// MapToListContactsInput selects contacts belonging to the first listed client.
func MapToListContactsInput(result harvest.ListClientsResult) harvest.ListContactsInput {
	if len(result.Value.Clients) == 0 {
		return harvest.ListContactsInput{PerPage: 10}
	}
	return harvest.ListContactsInput{ClientID: result.Value.Clients[0].ID, PerPage: 10}
}

// MapToGetCompanyInput selects the connected account.
func MapToGetCompanyInput(_ harvest.ListContactsResult) harvest.GetCompanyInput {
	return harvest.GetCompanyInput{}
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Start the Harvest read with the typed request."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartHarvestLookup" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](firstStepType), request), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Choose the related Harvest read."
type afterFirst struct {
	dex.StepDefaultsNoWaitFor[harvest.ListProjectsResult]
}

func (afterFirst) GetStepType() string { return "AfterHarvestFirst" }
func (afterFirst) Execute(_ dex.Context, result harvest.ListProjectsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.ListProjectsResult](projectStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Choose the Harvest time-entry read."
type afterProject struct {
	dex.StepDefaultsNoWaitFor[harvest.GetProjectResult]
}

func (afterProject) GetStepType() string { return "AfterHarvestProject" }
func (afterProject) Execute(_ dex.Context, result harvest.GetProjectResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.GetProjectResult](secondStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Choose the Harvest client read."
type afterTimeEntries struct {
	dex.StepDefaultsNoWaitFor[harvest.ListTimeEntriesResult]
}

func (afterTimeEntries) GetStepType() string { return "AfterHarvestTimeEntries" }
func (afterTimeEntries) Execute(_ dex.Context, result harvest.ListTimeEntriesResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.ListTimeEntriesResult](timeEntryStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Choose the Harvest client read."
type afterTimeEntry struct {
	dex.StepDefaultsNoWaitFor[harvest.GetTimeEntryResult]
}

func (afterTimeEntry) GetStepType() string { return "AfterHarvestTimeEntry" }
func (afterTimeEntry) Execute(_ dex.Context, result harvest.GetTimeEntryResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.GetTimeEntryResult](currentUserStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Choose the Harvest client read."
type afterCurrentUser struct {
	dex.StepDefaultsNoWaitFor[harvest.GetCurrentUserResult]
}

func (afterCurrentUser) GetStepType() string { return "AfterHarvestCurrentUser" }
func (afterCurrentUser) Execute(_ dex.Context, result harvest.GetCurrentUserResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.GetCurrentUserResult](clientStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Read contacts for the selected Harvest client."
type afterClients struct {
	dex.StepDefaultsNoWaitFor[harvest.ListClientsResult]
}

func (afterClients) GetStepType() string { return "AfterHarvestClients" }
func (afterClients) Execute(_ dex.Context, result harvest.ListClientsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.ListClientsResult](contactStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Read settings for the connected Harvest company."
type afterContacts struct {
	dex.StepDefaultsNoWaitFor[harvest.ListContactsResult]
}

func (afterContacts) GetStepType() string { return "AfterHarvestContacts" }
func (afterContacts) Execute(_ dex.Context, result harvest.ListContactsResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[harvest.ListContactsResult](companyStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Complete the Flow with Harvest data."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[harvest.GetCompanyResult]
}

func (completeLookup) GetStepType() string { return "CompleteHarvestLookup" }
func (completeLookup) Execute(_ dex.Context, result harvest.GetCompanyResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(Outcome{Company: result.Value}), nil
}
