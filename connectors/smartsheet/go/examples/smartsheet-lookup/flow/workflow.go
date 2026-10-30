// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package smartsheetlookup demonstrates two Smartsheet reads in a Dex Flow.
package smartsheetlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "SmartsheetLookup"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "smartsheet"
const firstStepType = "ReadSmartsheetFirst"
const secondStepType = "ReadSmartsheetSecond"

// Request is the typed Start Flow input.
type Request struct {
	// Page is the 1-based sheet page; zero selects the first.
	Page int `json:"page,omitempty"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Sheet contains the provider data returned by the Flow.
	Sheet smartsheet.Sheet `json:"sheet"`
}

// Flow reads the first and second provider resources.
type Flow struct {
	dex.FlowDefaults
	connection smartsheet.Connection
}

// NewFlow binds the static connection at registration.
func NewFlow(connection smartsheet.Connection) *Flow { return &Flow{connection: connection} }

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
		dex.DefineStep(smartsheet.NewListSheetsStep(smartsheet.ListSheetsStepConfig[Request]{StepType: firstStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "smartsheet-lookup", GroupLabel: "Smartsheet lookup", Explanation: "Read Smartsheet records."}, MapToOperationInput: MapToListSheetsInput, Listed: sdkgo.GoTo(afterFirst{})})),
		dex.DefineStep(afterFirst{}),
		dex.DefineStep(smartsheet.NewGetSheetStep(smartsheet.GetSheetStepConfig[smartsheet.GetSheetInput]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "smartsheet-lookup", GroupLabel: "Smartsheet lookup", Explanation: "Read the related Smartsheet resource."}, MapToOperationInput: MapToGetSheetInput, Found: sdkgo.GoTo(completeLookup{})})),
		dex.DefineStep(completeLookup{}),
	}
}

// MapToListSheetsInput maps the start input to the first read.
func MapToListSheetsInput(request Request) smartsheet.ListSheetsInput {
	return smartsheet.ListSheetsInput{Page: request.Page, PageSize: 10}
}

// MapToGetSheetInput maps the transition value to the second read.
func MapToGetSheetInput(input smartsheet.GetSheetInput) smartsheet.GetSheetInput { return input }

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Start the Smartsheet read with the typed request."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSmartsheetLookup" }
func (startRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](firstStepType), request), nil
}

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Choose the related Smartsheet read."
type afterFirst struct {
	dex.StepDefaultsNoWaitFor[smartsheet.ListSheetsResult]
}

func (afterFirst) GetStepType() string { return "AfterSmartsheetFirst" }
func (afterFirst) Execute(_ dex.Context, result smartsheet.ListSheetsResult) (*dex.StepDecision, error) {
	if len(result.Value.Sheets) == 0 {
		return dex.GracefulComplete(Outcome{}), nil
	}
	return dex.GoTo(sdkgo.StepRef[smartsheet.GetSheetInput](secondStepType), smartsheet.GetSheetInput{SheetID: result.Value.Sheets[0].ID}), nil
}

// dex:group group-id:smartsheet-lookup group-label:"Smartsheet lookup"
// dex:explanation text:"Complete the Flow with Smartsheet data."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[smartsheet.GetSheetResult]
}

func (completeLookup) GetStepType() string { return "CompleteSmartsheetLookup" }
func (completeLookup) Execute(_ dex.Context, result smartsheet.GetSheetResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(Outcome{Sheet: result.Value}), nil
}
