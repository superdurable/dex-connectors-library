// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package orderstatus runs Feihan queryRechargeOrder as one Dex Flow.
package orderstatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/feihan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FeihanRechargeOrder"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "feihan"
	operationStepType = "CallQueryRechargeOrder"
)

var orderStatusOutcomeAttribute = dex.DefineAttribute[feihan.RechargeOrderResult]("feihan-recharge-order-outcome")

// Flow runs QueryRechargeOrder once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection feihan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection feihan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(feihan.NewQueryRechargeOrderStep(feihan.QueryRechargeOrderStepConfig[feihan.QueryRechargeOrderInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Feihan", Explanation: "Query the status of a known recharge order."},
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

// dex:field attribute-key:feihan-recharge-order-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRechargeOrderResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"feihan-recharge-order-outcome": outcome}}, nil
}

// dex:field attribute-key:feihan-recharge-order-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRechargeOrderResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"feihan-recharge-order-outcome": outcome}}, nil
}

func readRechargeOrderResultOutcome(ctx dex.Context) (feihan.RechargeOrderResult, error) {
	outcome, err := orderStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return feihan.RechargeOrderResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input feihan.QueryRechargeOrderInput) feihan.QueryRechargeOrderInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryRechargeOrder" }
func (startRequest) WaitFor(dex.Context, feihan.QueryRechargeOrderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input feihan.QueryRechargeOrderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[feihan.QueryRechargeOrderInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[feihan.QueryRechargeOrderResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryRechargeOrder" }
func (completeResult) Execute(ctx dex.Context, result feihan.QueryRechargeOrderResult) (*dex.StepDecision, error) {
	if err := orderStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
