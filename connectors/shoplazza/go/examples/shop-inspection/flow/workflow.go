// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shopinspection runs Shoplazza getShop as one Dex Flow.
package shopinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/shoplazza/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ShoplazzaShopInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "shoplazza"
	operationStepType = "CallGetShop"
)

var shopOutcomeAttribute = dex.DefineAttribute[shoplazza.GetShopOutput]("shoplazza-shop-inspection-outcome")

// Flow runs GetShop once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection shoplazza.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection shoplazza.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shopOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(shoplazza.NewGetShopStep(shoplazza.GetShopStepConfig[shoplazza.GetShopInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Shoplazza", Explanation: "Read the configured store identity from Shoplazza."},
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

// dex:field attribute-key:shoplazza-shop-inspection-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopInspectionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shoplazza-shop-inspection-outcome": outcome}}, nil
}

// dex:field attribute-key:shoplazza-shop-inspection-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopInspectionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shoplazza-shop-inspection-outcome": outcome}}, nil
}

func readShopInspectionOutcome(ctx dex.Context) (shoplazza.GetShopOutput, error) {
	outcome, err := shopOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return shoplazza.GetShopOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input shoplazza.GetShopInput) shoplazza.GetShopInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start a shop metadata read with no user input."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetShop" }
func (startRequest) WaitFor(dex.Context, shoplazza.GetShopInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input shoplazza.GetShopInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[shoplazza.GetShopInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[shoplazza.GetShopResult]
}

func (completeResult) GetStepType() string { return "CompleteGetShop" }
func (completeResult) Execute(ctx dex.Context, result shoplazza.GetShopResult) (*dex.StepDecision, error) {
	if err := shopOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
