// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PriceFlowType is the stable Paddle price listing Flow identity.
const PriceFlowType = "PaddlePriceListing"

const priceStepType = "CallListPrices"

var pricePageAttribute = dex.DefineAttribute[paddle.PricePage]("paddle-price-page")

// PriceFlow lists one page of Paddle prices.
type PriceFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewPriceFlow binds the Paddle connection for listing prices.
func NewPriceFlow(connection paddle.Connection) *PriceFlow {
	return &PriceFlow{connection: connection}
}

func (*PriceFlow) GetFlowType() string { return PriceFlowType }

func (*PriceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{pricePageAttribute}}
}

func (flow *PriceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPriceList{}),
		dex.DefineStep(paddle.NewListPricesStep(paddle.ListPricesStepConfig[paddle.ListPricesInput]{
			StepType: priceStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle prices."},
			Connection:  flow.connection, MapToOperationInput: mapPriceListInput,
			Listed: sdkgo.GoTo(completePriceList{}),
		})),
		dex.DefineStep(completePriceList{}),
	}
}

func (flow *PriceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-price-page value-type:json editable:false description:"Price page"
func (*PriceFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPricePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-price-page": page}}, nil
}

// dex:field attribute-key:paddle-price-page value-type:json editable:false description:"Price page"
func (*PriceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPricePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-price-page": page}}, nil
}

func readPricePage(ctx dex.Context) (paddle.PricePage, error) {
	page, err := pricePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.PricePage{}, nil
	}
	return page, err
}

func mapPriceListInput(input paddle.ListPricesInput) paddle.ListPricesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested price page and cursor to Paddle."
type startPriceList struct{ dex.StepDefaults }

func (startPriceList) GetStepType() string { return "StartListPrices" }
func (startPriceList) WaitFor(dex.Context, paddle.ListPricesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPriceList) Execute(_ dex.Context, input paddle.ListPricesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListPricesInput](priceStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned price page."
type completePriceList struct {
	dex.StepDefaultsNoWaitFor[paddle.ListPricesResult]
}

func (completePriceList) GetStepType() string { return "CompleteListPrices" }
func (completePriceList) Execute(ctx dex.Context, result paddle.ListPricesResult) (*dex.StepDecision, error) {
	if err := pricePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PriceFlow)(nil)
