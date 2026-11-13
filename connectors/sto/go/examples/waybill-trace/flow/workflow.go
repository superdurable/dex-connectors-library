// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package waybilltrace runs STO queryWaybillTrace as one Dex Flow.
package waybilltrace

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sto/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "STOWaybillTrace"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "sto"
	operationStepType = "CallQueryWaybillTrace"
)

var traceAttribute = dex.DefineAttribute[sto.WaybillTrace]("sto-waybill-trace")

// Flow runs QueryWaybillTrace once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sto.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sto.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{traceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sto.NewQueryWaybillTraceStep(sto.QueryWaybillTraceStepConfig[sto.QueryWaybillTraceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "STO", Explanation: "Read the authorized STO waybill trace."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sto-waybill-trace value-type:json editable:false description:"STO waybill trace information"
// GetDexSummary shows the last STO waybill trace information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrace(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sto-waybill-trace": outcome}}, nil
}

// dex:field attribute-key:sto-waybill-trace value-type:json editable:false description:"STO waybill trace information"
// GetDexDisplay shows the last STO waybill trace information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrace(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sto-waybill-trace": outcome}}, nil
}

func readTrace(ctx dex.Context) (sto.WaybillTrace, error) {
	outcome, err := traceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sto.WaybillTrace{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input sto.QueryWaybillTraceInput) sto.QueryWaybillTraceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryWaybillTrace" }
func (startRequest) WaitFor(dex.Context, sto.QueryWaybillTraceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sto.QueryWaybillTraceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sto.QueryWaybillTraceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sto.QueryWaybillTraceResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryWaybillTrace" }
func (completeResult) Execute(ctx dex.Context, result sto.QueryWaybillTraceResult) (*dex.StepDecision, error) {
	if err := traceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
