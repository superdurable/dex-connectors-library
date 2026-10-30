// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package closelookup demonstrates two Close reads in a Dex Flow.
package closelookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/close"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FlowType is the stable Dex Flow identity.
const FlowType = "CloseLookup"

// ConnectionName is the static Dex Web connection name.
const ConnectionName = "close"
const firstStepType = "ReadCloseFirst"
const secondStepType = "ReadCloseSecond"

// Request is the typed Start Flow input.
type Request struct {
	// Skip is the zero-based lead offset.
	Skip int `json:"skip,omitempty"`
}

// Outcome is the Flow completion output.
type Outcome struct {
	// Lead contains the provider data returned by the Flow.
	Lead close.Lead `json:"lead"`
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
		dex.DefineStep(close.NewListLeadsStep(close.ListLeadsStepConfig[Request]{StepType: firstStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read Close records."}, MapToOperationInput: MapToListLeadsInput, Listed: sdkgo.GoTo(afterFirst{})})),
		dex.DefineStep(afterFirst{}),
		dex.DefineStep(close.NewGetLeadStep(close.GetLeadStepConfig[close.GetLeadInput]{StepType: secondStepType, ConnectionName: ConnectionName, Connection: flow.connection, Annotations: sdkgo.StepAnnotations{GroupID: "close-lookup", GroupLabel: "Close lookup", Explanation: "Read the related Close resource."}, MapToOperationInput: MapToGetLeadInput, Found: sdkgo.GoTo(completeLookup{})})),
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
// dex:explanation text:"Complete the Flow with Close data."
type completeLookup struct {
	dex.StepDefaultsNoWaitFor[close.GetLeadResult]
}

func (completeLookup) GetStepType() string { return "CompleteCloseLookup" }
func (completeLookup) Execute(_ dex.Context, result close.GetLeadResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(Outcome{Lead: result.Value}), nil
}
