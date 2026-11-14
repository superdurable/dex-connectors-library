// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shopcatalog runs Wangdian listShops as one Dex Flow.
package shopcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wangdian/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WangdianShopCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "wangdian"
	operationStepType = "CallListShops"
)

var shopCatalogOutcomeAttribute = dex.DefineAttribute[wangdian.ListShopsOutput]("wangdian-shop-directory-outcome")

// Flow runs ListShops once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection wangdian.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection wangdian.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shopCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wangdian.NewListShopsStep(wangdian.ListShopsStepConfig[wangdian.ListShopsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wangdian", Explanation: "Read one ERP shop page with IDs, names, platform IDs, and authorization states."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wangdian-shop-directory-outcome value-type:json editable:false description:"Shop catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wangdian-shop-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:wangdian-shop-directory-outcome value-type:json editable:false description:"Shop catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readShopCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wangdian-shop-directory-outcome": outcome}}, nil
}

func readShopCatalogOutcome(ctx dex.Context) (wangdian.ListShopsOutput, error) {
	outcome, err := shopCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wangdian.ListShopsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input wangdian.ListShopsInput) wangdian.ListShopsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListShops" }
func (startRequest) WaitFor(dex.Context, wangdian.ListShopsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wangdian.ListShopsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wangdian.ListShopsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wangdian.ListShopsResult]
}

func (completeResult) GetStepType() string { return "CompleteListShops" }
func (completeResult) Execute(ctx dex.Context, result wangdian.ListShopsResult) (*dex.StepDecision, error) {
	if err := shopCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
