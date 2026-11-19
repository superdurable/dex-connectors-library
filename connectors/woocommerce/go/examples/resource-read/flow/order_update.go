// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/woocommerce/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// OrderUpdateRequest identifies an order and its replacement status.
type OrderUpdateRequest struct {
	// OrderID identifies the provider order.
	OrderID int64 `json:"orderId"`
	// Status is the replacement WooCommerce order status.
	Status string `json:"status"`
}

// OrderUpdateFlow demonstrates an absolute order-status replacement.
type OrderUpdateFlow struct {
	dex.FlowDefaults
	connection woocommerce.Connection
}

var orderUpdateOutcomeAttribute = dex.DefineAttribute[woocommerce.Order]("woocommerce-order-update-outcome")

// NewOrderUpdateFlow binds the same static connection as the read example.
func NewOrderUpdateFlow(connection woocommerce.Connection) *OrderUpdateFlow {
	return &OrderUpdateFlow{connection: connection}
}

// GetPersistenceSchema registers the persisted provider outcome.
func (*OrderUpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderUpdateOutcomeAttribute}}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *OrderUpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:woocommerce-order-update-outcome value-type:json editable:false description:"Updated order"
// GetDexSummary reads the last confirmed order update.
func (*OrderUpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := orderUpdateOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"woocommerce-order-update-outcome": order}}, nil
}

// dex:field attribute-key:woocommerce-order-update-outcome value-type:json editable:false description:"Updated order"
// GetDexDisplay reads the last confirmed order update.
func (*OrderUpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := orderUpdateOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"woocommerce-order-update-outcome": order}}, nil
}

// GetSteps wires the provider Mutation and completion Step.
func (flow *OrderUpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareOrderUpdate{}),
		dex.DefineStep(woocommerce.NewUpdateOrderStep(woocommerce.UpdateOrderStepConfig[OrderUpdateRequest]{
			StepType: "UpdateWooCommerceOrder", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "order-update", GroupLabel: "Order update", Explanation: "Replace the order status in WooCommerce."},
			MapToOperationInput: mapToOrderUpdateInput,
			Updated:             sdkgo.GoTo(completeOrderUpdate{}),
		})),
		dex.DefineStep(completeOrderUpdate{}),
	}
}

func mapToOrderUpdateInput(request OrderUpdateRequest) woocommerce.UpdateOrderInput {
	return woocommerce.UpdateOrderInput{OrderID: request.OrderID, Status: request.Status}
}

// dex:group group-id:order-update group-label:"Order update"
// dex:explanation text:"Validate the target order and status before the provider write."
type prepareOrderUpdate struct{ dex.StepDefaults }

func (prepareOrderUpdate) WaitFor(dex.Context, OrderUpdateRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareOrderUpdate) Execute(_ dex.Context, request OrderUpdateRequest) (*dex.StepDecision, error) {
	if request.OrderID <= 0 || request.Status == "" {
		return dex.ForceFail("orderId and status are required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[OrderUpdateRequest]("UpdateWooCommerceOrder"), request), nil
}

// dex:group group-id:order-update-result group-label:"Order update result"
// dex:explanation text:"Complete the Flow after WooCommerce confirms the order update."
type completeOrderUpdate struct {
	dex.StepDefaultsNoWaitFor[woocommerce.UpdateOrderResult]
}

func (completeOrderUpdate) Execute(ctx dex.Context, result woocommerce.UpdateOrderResult) (*dex.StepDecision, error) {
	if err := orderUpdateOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*OrderUpdateFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*OrderUpdateFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*OrderUpdateFlow)(nil).GetDexDisplay
