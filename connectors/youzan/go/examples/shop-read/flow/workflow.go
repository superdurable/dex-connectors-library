// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shopread runs Youzan getShopBasic as one Dex Flow.
package shopread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/youzan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "YouzanShopRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "youzan"
	operationStepType = "CallGetShopBasic"
)

var shopAttribute = dex.DefineAttribute[youzan.ShopBasic]("youzan-shop-read")

// Flow runs GetShopBasic once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection youzan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection youzan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shopAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(youzan.NewGetShopBasicStep(youzan.GetShopBasicStepConfig[youzan.GetShopBasicInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Youzan", Explanation: "Read the authorized Youzan shop."},
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

// dex:field attribute-key:youzan-shop-read value-type:json editable:false description:"Youzan shop information"
// GetDexSummary shows the last Youzan shop information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShop(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"youzan-shop-read": outcome}}, nil
}

// dex:field attribute-key:youzan-shop-read value-type:json editable:false description:"Youzan shop information"
// GetDexDisplay shows the last Youzan shop information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShop(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"youzan-shop-read": outcome}}, nil
}

func readShop(ctx dex.Context) (youzan.ShopBasic, error) {
	outcome, err := shopAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return youzan.ShopBasic{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input youzan.GetShopBasicInput) youzan.GetShopBasicInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetShopBasic" }
func (startRequest) WaitFor(dex.Context, youzan.GetShopBasicInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input youzan.GetShopBasicInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[youzan.GetShopBasicInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[youzan.GetShopBasicResult]
}

func (completeResult) GetStepType() string { return "CompleteGetShopBasic" }
func (completeResult) Execute(ctx dex.Context, result youzan.GetShopBasicResult) (*dex.StepDecision, error) {
	if err := shopAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
