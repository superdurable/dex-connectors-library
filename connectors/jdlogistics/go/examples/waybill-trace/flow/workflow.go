// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package waybilltrace runs JD Logistics queryWaybillTrace as one Dex Flow.
package waybilltrace

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/jdlogistics/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JDLogisticsWaybillTrace"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jdlogistics"
	operationStepType = "CallQueryWaybillTrace"
)

var traceAttribute = dex.DefineAttribute[jdlogistics.WaybillTrace]("jdlogistics-waybill-trace")

// Flow runs QueryWaybillTrace once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jdlogistics.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jdlogistics.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{traceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jdlogistics.NewQueryWaybillTraceStep(jdlogistics.QueryWaybillTraceStepConfig[jdlogistics.QueryWaybillTraceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "JD Logistics", Explanation: "Read the authorized JD Logistics waybill trace."},
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

// dex:field attribute-key:jdlogistics-waybill-trace value-type:json editable:false description:"JD Logistics waybill trace information"
// GetDexSummary shows the last JD Logistics waybill trace information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrace(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jdlogistics-waybill-trace": outcome}}, nil
}

// dex:field attribute-key:jdlogistics-waybill-trace value-type:json editable:false description:"JD Logistics waybill trace information"
// GetDexDisplay shows the last JD Logistics waybill trace information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTrace(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jdlogistics-waybill-trace": outcome}}, nil
}

func readTrace(ctx dex.Context) (jdlogistics.WaybillTrace, error) {
	outcome, err := traceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jdlogistics.WaybillTrace{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input jdlogistics.QueryWaybillTraceInput) jdlogistics.QueryWaybillTraceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryWaybillTrace" }
func (startRequest) WaitFor(dex.Context, jdlogistics.QueryWaybillTraceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jdlogistics.QueryWaybillTraceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jdlogistics.QueryWaybillTraceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jdlogistics.QueryWaybillTraceResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryWaybillTrace" }
func (completeResult) Execute(ctx dex.Context, result jdlogistics.QueryWaybillTraceResult) (*dex.StepDecision, error) {
	if err := traceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
