// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flow demonstrates Coda's listRows operation in a Dex Flow.
package flow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/coda"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow type.
	FlowType = "CodaSample"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "coda"
	callStepType   = "CallCoda"
)

var sampleResultAttribute = dex.DefineAttribute[coda.ListRowsOutput]("coda-sample-result")

// Flow calls Coda once and completes with the provider result.
type Flow struct {
	dex.FlowDefaults
	connection coda.Connection
}

// NewFlow binds the connector connection at Worker registration.
func NewFlow(connection coda.Connection) *Flow { return &Flow{connection: connection} }

// GetPersistenceSchema registers the completed provider result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sampleResultAttribute}}
}

// GetFlowType returns the sample Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes the result to Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the completed provider result when available.
// dex:field attribute-key:coda-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"coda-sample-result": result}}, nil
}

// GetDexDisplay returns the completed provider result when available.
// dex:field attribute-key:coda-sample-result value-type:json editable:false description:"Provider result"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := inspectResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"coda-sample-result": result}}, nil
}

func inspectResult(ctx dex.Context) (coda.ListRowsOutput, error) {
	result, err := sampleResultAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return coda.ListRowsOutput{}, err
	}
	return result, nil
}

// GetSteps registers the start, provider, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(coda.NewListRowsStep(coda.ListRowsStepConfig[coda.ListRowsInput]{
			StepType: callStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "coda", GroupLabel: "Coda", Explanation: "List one page of rows in a Coda table."},
			MapToOperationInput: MapToOperationInput, Listed: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// MapToOperationInput passes the validated Flow request to the connector.
func MapToOperationInput(request coda.ListRowsInput) coda.ListRowsInput { return request }

// dex:group group-id:coda-start group-label:"Coda sample"
// dex:explanation text:"Pass the start input to the connector operation."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCodaRequest" }
func (startRequest) WaitFor(dex.Context, coda.ListRowsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, request coda.ListRowsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[coda.ListRowsInput](callStepType), request), nil
}

// dex:group group-id:coda-complete group-label:"Coda sample"
// dex:explanation text:"Complete the Flow with the provider response."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[coda.ListRowsResult]
}

func (completeRequest) GetStepType() string { return "CompleteCodaRequest" }
func (completeRequest) Execute(ctx dex.Context, result coda.ListRowsResult) (*dex.StepDecision, error) {
	if err := sampleResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
