// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package productsearch runs SMZDM searchProducts as one Dex Flow.
package productsearch

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/smzdm/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SMZDMProductSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "smzdm"
	operationStepType = "CallSearchProducts"
)

var productListOutcomeAttribute = dex.DefineAttribute[smzdm.ProductPage]("smzdm-product-search-outcome")

// Flow runs SearchProducts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection smzdm.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection smzdm.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{productListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(smzdm.NewSearchProductsStep(smzdm.SearchProductsStepConfig[smzdm.SearchProductsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SMZDM", Explanation: "Search the SMZDM product index."},
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

// dex:field attribute-key:smzdm-product-search-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProductsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"smzdm-product-search-outcome": outcome}}, nil
}

// dex:field attribute-key:smzdm-product-search-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProductsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"smzdm-product-search-outcome": outcome}}, nil
}

func readProductsOutcome(ctx dex.Context) (smzdm.ProductPage, error) {
	outcome, err := productListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return smzdm.ProductPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input smzdm.SearchProductsInput) smzdm.SearchProductsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchProducts" }
func (startRequest) WaitFor(dex.Context, smzdm.SearchProductsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input smzdm.SearchProductsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[smzdm.SearchProductsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[smzdm.SearchProductsResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchProducts" }
func (completeResult) Execute(ctx dex.Context, result smzdm.SearchProductsResult) (*dex.StepDecision, error) {
	if err := productListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
