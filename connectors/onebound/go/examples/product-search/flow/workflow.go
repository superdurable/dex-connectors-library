// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package productsearch runs Onebound searchProducts as one Dex Flow.
package productsearch

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/onebound/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OneboundProductSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "onebound"
	operationStepType = "CallSearchProducts"
)

var productSearchOutcomeAttribute = dex.DefineAttribute[onebound.ProductSearchPage]("onebound-product-list-outcome")

// Flow runs SearchProducts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection onebound.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection onebound.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{productSearchOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(onebound.NewSearchProductsStep(onebound.SearchProductsStepConfig[onebound.SearchProductsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Onebound", Explanation: "Search Onebound public 1688 products."},
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

// dex:field attribute-key:onebound-product-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProductSearchPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onebound-product-list-outcome": outcome}}, nil
}

// dex:field attribute-key:onebound-product-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProductSearchPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onebound-product-list-outcome": outcome}}, nil
}

func readProductSearchPageOutcome(ctx dex.Context) (onebound.ProductSearchPage, error) {
	outcome, err := productSearchOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return onebound.ProductSearchPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input onebound.SearchProductsInput) onebound.SearchProductsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchProducts" }
func (startRequest) WaitFor(dex.Context, onebound.SearchProductsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input onebound.SearchProductsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[onebound.SearchProductsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[onebound.SearchProductsResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchProducts" }
func (completeResult) Execute(ctx dex.Context, result onebound.SearchProductsResult) (*dex.StepDecision, error) {
	if err := productSearchOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
