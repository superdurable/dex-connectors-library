// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentlinkinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adyen/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// LookupCardBrandsFlowType identifies the BIN-only example Flow.
const LookupCardBrandsFlowType = "AdyenLookupCardBrandsByBIN"

const lookupCardBrandsStepType = "CallLookupCardBrandsByBIN"

var cardBrandsAttribute = dex.DefineAttribute[adyen.CardBrands]("adyen-card-brands")

// LookupCardBrandsFlow reads supported card brands from a six-digit issuer prefix.
type LookupCardBrandsFlow struct {
	dex.FlowDefaults
	connection adyen.Connection
}

// NewLookupCardBrandsFlow binds the static Adyen connection.
func NewLookupCardBrandsFlow(connection adyen.Connection) *LookupCardBrandsFlow {
	return &LookupCardBrandsFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*LookupCardBrandsFlow) GetFlowType() string { return LookupCardBrandsFlowType }

// GetPersistenceSchema stores only scheme and support fields.
func (*LookupCardBrandsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{cardBrandsAttribute}}
}

// GetSteps registers the BIN-only Query and completion path.
func (flow *LookupCardBrandsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLookupCardBrands{}),
		dex.DefineStep(adyen.NewLookupCardBrandsByBINStep(adyen.LookupCardBrandsByBINStepConfig[adyen.LookupCardBrandsByBINInput]{
			StepType: lookupCardBrandsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Adyen", Explanation: "Look up card brand support from a six-digit issuer prefix."},
			Connection:          flow.connection,
			MapToOperationInput: func(input adyen.LookupCardBrandsByBINInput) adyen.LookupCardBrandsByBINInput { return input },
			Found:               sdkgo.GoTo(completeLookupCardBrands{}),
		})),
		dex.DefineStep(completeLookupCardBrands{}),
	}
}

// GetRPCs exposes sanitized brand results in Dex Web.
func (flow *LookupCardBrandsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:adyen-card-brands value-type:json editable:false description:"Supported card brands"
// GetDexSummary shows supported card brands.
func (*LookupCardBrandsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	brands, err := readCardBrands(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-card-brands": brands}}, nil
}

// dex:field attribute-key:adyen-card-brands value-type:json editable:false description:"Supported card brands"
// GetDexDisplay shows supported card brands.
func (*LookupCardBrandsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	brands, err := readCardBrands(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"adyen-card-brands": brands}}, nil
}

func readCardBrands(ctx dex.Context) (adyen.CardBrands, error) {
	brands, err := cardBrandsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return adyen.CardBrands{}, nil
	}
	return brands, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass only the six-digit issuer prefix to the Query."
type startLookupCardBrands struct{ dex.StepDefaults }

func (startLookupCardBrands) GetStepType() string { return "StartLookupCardBrands" }
func (startLookupCardBrands) WaitFor(dex.Context, adyen.LookupCardBrandsByBINInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLookupCardBrands) Execute(_ dex.Context, input adyen.LookupCardBrandsByBINInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[adyen.LookupCardBrandsByBINInput](lookupCardBrandsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with supported card-brand codes."
type completeLookupCardBrands struct {
	dex.StepDefaultsNoWaitFor[adyen.LookupCardBrandsByBINResult]
}

func (completeLookupCardBrands) GetStepType() string { return "CompleteLookupCardBrands" }
func (completeLookupCardBrands) Execute(ctx dex.Context, result adyen.LookupCardBrandsByBINResult) (*dex.StepDecision, error) {
	if err := cardBrandsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LookupCardBrandsFlow)(nil)
