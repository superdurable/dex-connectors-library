// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package orderstatus runs Meituan queryOrderStatus as one Dex Flow.
package orderstatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/meituan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MeituanOrderStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "meituan"
	operationStepType = "CallQueryOrderStatus"
)

var orderStatusOutcomeAttribute = dex.DefineAttribute[meituan.OrderStatusResult]("meituan-order-status-outcome")

// Flow runs QueryOrderStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection meituan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection meituan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(meituan.NewQueryOrderStatusStep(meituan.QueryOrderStatusStepConfig[meituan.QueryOrderStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Meituan", Explanation: "Query the status of a known delivery order."},
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

// dex:field attribute-key:meituan-order-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrderStatusResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meituan-order-status-outcome": outcome}}, nil
}

// dex:field attribute-key:meituan-order-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrderStatusResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meituan-order-status-outcome": outcome}}, nil
}

func readOrderStatusResultOutcome(ctx dex.Context) (meituan.OrderStatusResult, error) {
	outcome, err := orderStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meituan.OrderStatusResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input meituan.QueryOrderStatusInput) meituan.QueryOrderStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryOrderStatus" }
func (startRequest) WaitFor(dex.Context, meituan.QueryOrderStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input meituan.QueryOrderStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meituan.QueryOrderStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[meituan.QueryOrderStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryOrderStatus" }
func (completeResult) Execute(ctx dex.Context, result meituan.QueryOrderStatusResult) (*dex.StepDecision, error) {
	if err := orderStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
