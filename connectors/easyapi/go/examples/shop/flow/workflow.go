// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shopflow runs EasyAPI queryShop as one Dex Flow.
package shopflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/easyapi/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EasyAPIShop"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "easyapi"
	operationStepType = "CallQueryShop"
)

var shopOutcomeAttribute = dex.DefineAttribute[easyapi.ShopResult]("easyapi-shop-outcome")

// Flow runs QueryShop once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection easyapi.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection easyapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shopOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(easyapi.NewQueryShopStep(easyapi.QueryShopStepConfig[easyapi.QueryShopInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "EasyAPI", Explanation: "Query the status of a authorized invoicing company."},
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

// dex:field attribute-key:easyapi-shop-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"easyapi-shop-outcome": outcome}}, nil
}

// dex:field attribute-key:easyapi-shop-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopResultOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"easyapi-shop-outcome": outcome}}, nil
}

func readShopResultOutcome(ctx dex.Context) (easyapi.ShopResult, error) {
	outcome, err := shopOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return easyapi.ShopResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input easyapi.QueryShopInput) easyapi.QueryShopInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryShop" }
func (startRequest) WaitFor(dex.Context, easyapi.QueryShopInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input easyapi.QueryShopInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[easyapi.QueryShopInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[easyapi.QueryShopResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryShop" }
func (completeResult) Execute(ctx dex.Context, result easyapi.QueryShopResult) (*dex.StepDecision, error) {
	if err := shopOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
