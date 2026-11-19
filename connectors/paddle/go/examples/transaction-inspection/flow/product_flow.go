// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProductFlowType is the stable Paddle product listing Flow identity.
const ProductFlowType = "PaddleProductListing"

const productStepType = "CallListProducts"

var productPageAttribute = dex.DefineAttribute[paddle.ProductPage]("paddle-product-page")

// ProductFlow lists one page of Paddle products.
type ProductFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewProductFlow binds the Paddle connection for listing products.
func NewProductFlow(connection paddle.Connection) *ProductFlow {
	return &ProductFlow{connection: connection}
}

func (*ProductFlow) GetFlowType() string { return ProductFlowType }

func (*ProductFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{productPageAttribute}}
}

func (flow *ProductFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProductList{}),
		dex.DefineStep(paddle.NewListProductsStep(paddle.ListProductsStepConfig[paddle.ListProductsInput]{
			StepType: productStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle products."},
			Connection:  flow.connection, MapToOperationInput: mapProductListInput,
			Listed: sdkgo.GoTo(completeProductList{}),
		})),
		dex.DefineStep(completeProductList{}),
	}
}

func (flow *ProductFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-product-page value-type:json editable:false description:"Product page"
func (*ProductFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProductPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-product-page": page}}, nil
}

// dex:field attribute-key:paddle-product-page value-type:json editable:false description:"Product page"
func (*ProductFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProductPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-product-page": page}}, nil
}

func readProductPage(ctx dex.Context) (paddle.ProductPage, error) {
	page, err := productPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.ProductPage{}, nil
	}
	return page, err
}

func mapProductListInput(input paddle.ListProductsInput) paddle.ListProductsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested product page and cursor to Paddle."
type startProductList struct{ dex.StepDefaults }

func (startProductList) GetStepType() string { return "StartListProducts" }
func (startProductList) WaitFor(dex.Context, paddle.ListProductsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProductList) Execute(_ dex.Context, input paddle.ListProductsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListProductsInput](productStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned product page."
type completeProductList struct {
	dex.StepDefaultsNoWaitFor[paddle.ListProductsResult]
}

func (completeProductList) GetStepType() string { return "CompleteListProducts" }
func (completeProductList) Execute(ctx dex.Context, result paddle.ListProductsResult) (*dex.StepDecision, error) {
	if err := productPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProductFlow)(nil)
