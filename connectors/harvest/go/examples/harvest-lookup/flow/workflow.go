// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package harvestlookup demonstrates two Harvest reads in a Dex Flow.
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
const secondStepType = "ReadHarvestSecond"

// Request is the typed Start Flow input.
type Request struct {
	// PerPage caps the projects returned in the first page.
	PerPage int `json:"perPage,omitempty"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// TimeEntries contains the provider data returned by the Flow.
	TimeEntries []harvest.TimeEntry `json:"timeEntries"`
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
		dex.DefineStep(harvest.NewListTimeEntriesStep(harvest.ListTimeEntriesStepConfig[harvest.ListProjectsResult]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "harvest-lookup", GroupLabel: "Harvest lookup", Explanation: "Read the related Harvest resource."}, MapToOperationInput: MapToListTimeEntriesInput, Listed: sdkgo.GoTo(completeLookup{})})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToListProjectsInput maps the start input to the first read.
func MapToListProjectsInput(request Request) harvest.ListProjectsInput {
	return harvest.ListProjectsInput{PerPage: request.PerPage}
}

// MapToListTimeEntriesInput maps the transition value to the second read.
func MapToListTimeEntriesInput(input harvest.ListProjectsResult) harvest.ListTimeEntriesInput {
	return harvest.ListTimeEntriesInput{PerPage: 10}
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
	return dex.GoTo(sdkgo.StepRef[harvest.ListProjectsResult](secondStepType), result), nil
}

// dex:group group-id:harvest-lookup group-label:"Harvest lookup"
// dex:explanation text:"Complete the Flow with Harvest data."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[harvest.ListTimeEntriesResult]
}

func (completeLookup) GetStepType() string { return "CompleteHarvestLookup" }
func (completeLookup) Execute(_ dex.Context, result harvest.ListTimeEntriesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(Outcome{TimeEntries: result.Value.TimeEntries}), nil
}
