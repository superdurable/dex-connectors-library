// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/shopify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateProductFlowType is the stable product creation Flow identity.
const CreateProductFlowType = "ShopifyCreateProduct"

// CreateProductOutcome contains the created product summary.
type CreateProductOutcome struct {
	// Product is the created product without variants or images.
	Product shopify.Product `json:"product"`
}

var createProductOutcomeAttribute = dex.DefineAttribute[CreateProductOutcome]("shopify-created-product")

// CreateProductFlow creates a product on the static connection.
type CreateProductFlow struct {
	dex.FlowDefaults
	connection shopify.Connection
}

// NewCreateProductFlow binds the connection at registration.
func NewCreateProductFlow(connection shopify.Connection) *CreateProductFlow {
	return &CreateProductFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*CreateProductFlow) GetFlowType() string { return CreateProductFlowType }

// GetPersistenceSchema registers the created product Attribute.
func (*CreateProductFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createProductOutcomeAttribute}}
}

// GetSteps defines the create Mutation and completion Step.
func (flow *CreateProductFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(shopify.NewCreateProductStep(shopify.CreateProductStepConfig[shopify.CreateProductInput]{
			StepType: "CreateShopifyProduct", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "shopify", GroupLabel: "Shopify", Explanation: "Create a product with bounded duplicate sends."},
			MapToOperationInput: func(input shopify.CreateProductInput) shopify.CreateProductInput { return input },
			Created:             sdkgo.GoTo(completeProductCreation{}),
		})),
		dex.DefineStep(completeProductCreation{}),
	}
}

// GetRPCs exposes the summary and display RPCs.
func (flow *CreateProductFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the persisted product for Dex Web.
// dex:field attribute-key:shopify-created-product value-type:json editable:false description:"Created product"
func (*CreateProductFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := createProductOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-created-product": outcome}}, nil
}

// GetDexDisplay returns the persisted product for Dex Web.
// dex:field attribute-key:shopify-created-product value-type:json editable:false description:"Created product"
func (*CreateProductFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := createProductOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-created-product": outcome}}, nil
}

// dex:group group-id:product-create-result group-label:"Created product"
// dex:explanation text:"Persist the created product summary and complete the Flow."
type completeProductCreation struct {
	dex.StepDefaultsNoWaitFor[shopify.CreateProductResult]
}

func (completeProductCreation) GetStepType() string { return "CompleteShopifyProductCreation" }
func (completeProductCreation) Execute(ctx dex.Context, result shopify.CreateProductResult) (*dex.StepDecision, error) {
	outcome := CreateProductOutcome{Product: result.Value}
	if err := createProductOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*CreateProductFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*CreateProductFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*CreateProductFlow)(nil).GetDexDisplay
