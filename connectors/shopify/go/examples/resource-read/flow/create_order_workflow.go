// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/shopify/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CreateOrderFlowType is the stable order creation Flow identity.
const CreateOrderFlowType = "ShopifyCreateOrder"

// CreateOrderOutcome contains the created order summary.
type CreateOrderOutcome struct {
	// Order omits protected customer fields.
	Order shopify.Order `json:"order"`
}

var createOrderOutcomeAttribute = dex.DefineAttribute[CreateOrderOutcome]("shopify-created-order")

// CreateOrderFlow creates an unpaid order on the static connection.
type CreateOrderFlow struct {
	dex.FlowDefaults
	connection shopify.Connection
}

// NewCreateOrderFlow binds the connection at registration.
func NewCreateOrderFlow(connection shopify.Connection) *CreateOrderFlow {
	return &CreateOrderFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*CreateOrderFlow) GetFlowType() string { return CreateOrderFlowType }

// GetPersistenceSchema registers the created order Attribute.
func (*CreateOrderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createOrderOutcomeAttribute}}
}

// GetSteps defines the create Mutation and completion Step.
func (flow *CreateOrderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(shopify.NewCreateOrderStep(shopify.CreateOrderStepConfig[shopify.CreateOrderInput]{
			StepType: "CreateShopifyOrder", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "shopify", GroupLabel: "Shopify", Explanation: "Create an unpaid order with bounded duplicate sends."},
			MapToOperationInput: func(input shopify.CreateOrderInput) shopify.CreateOrderInput { return input },
			Created:             sdkgo.GoTo(completeOrderCreation{}),
		})),
		dex.DefineStep(completeOrderCreation{}),
	}
}

// GetRPCs exposes the summary and display RPCs.
func (flow *CreateOrderFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the persisted order for Dex Web.
// dex:field attribute-key:shopify-created-order value-type:json editable:false description:"Created order"
func (*CreateOrderFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := createOrderOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-created-order": outcome}}, nil
}

// GetDexDisplay returns the persisted order for Dex Web.
// dex:field attribute-key:shopify-created-order value-type:json editable:false description:"Created order"
func (*CreateOrderFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := createOrderOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shopify-created-order": outcome}}, nil
}

// dex:group group-id:order-create-result group-label:"Created order"
// dex:explanation text:"Persist the created order summary and complete the Flow."
type completeOrderCreation struct {
	dex.StepDefaultsNoWaitFor[shopify.CreateOrderResult]
}

func (completeOrderCreation) GetStepType() string { return "CompleteShopifyOrderCreation" }
func (completeOrderCreation) Execute(ctx dex.Context, result shopify.CreateOrderResult) (*dex.StepDecision, error) {
	outcome := CreateOrderOutcome{Order: result.Value}
	if err := createOrderOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*CreateOrderFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*CreateOrderFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*CreateOrderFlow)(nil).GetDexDisplay
