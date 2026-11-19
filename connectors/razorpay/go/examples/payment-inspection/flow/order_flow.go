// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package paymentinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/razorpay/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createOrderFlowType = "RazorpayCreateOrder"
const createOrderStepType = "CallCreateOrder"

var orderAttribute = dex.DefineAttribute[razorpay.OrderSummary]("razorpay-created-order")

// CreateOrderFlow creates one order with a call-specific receipt.
type CreateOrderFlow struct {
	dex.FlowDefaults
	connection razorpay.Connection
}

// NewCreateOrderFlow binds the static Razorpay connection.
func NewCreateOrderFlow(connection razorpay.Connection) *CreateOrderFlow {
	return &CreateOrderFlow{connection: connection}
}

func (*CreateOrderFlow) GetFlowType() string { return createOrderFlowType }

func (*CreateOrderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderAttribute}}
}

func (flow *CreateOrderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateOrder{}),
		dex.DefineStep(razorpay.NewCreateOrderStep(razorpay.CreateOrderStepConfig[razorpay.CreateOrderInput]{
			StepType: createOrderStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Razorpay", Explanation: "Create one order with a call-specific receipt."},
			Connection:          flow.connection,
			MapToOperationInput: mapCreateOrderInput,
			Created:             sdkgo.GoTo(completeCreateOrder{}),
		})),
		dex.DefineStep(completeCreateOrder{}),
	}
}

func (flow *CreateOrderFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:razorpay-created-order value-type:json editable:false description:"Created order"
// GetDexSummary returns the safe order summary for Dex Web.
func (*CreateOrderFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedOrder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-order": value}}, nil
}

// dex:field attribute-key:razorpay-created-order value-type:json editable:false description:"Created order"
// GetDexDisplay returns the safe order summary for Dex Web.
func (*CreateOrderFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedOrder(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"razorpay-created-order": value}}, nil
}

func readCreatedOrder(ctx dex.Context) (razorpay.OrderSummary, error) {
	value, err := orderAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return razorpay.OrderSummary{}, nil
	}
	return value, err
}

func mapCreateOrderInput(input razorpay.CreateOrderInput) razorpay.CreateOrderInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass amount and currency to the provider Step."
type startCreateOrder struct{ dex.StepDefaults }

func (startCreateOrder) GetStepType() string { return "StartCreateOrder" }
func (startCreateOrder) WaitFor(dex.Context, razorpay.CreateOrderInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateOrder) Execute(_ dex.Context, input razorpay.CreateOrderInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[razorpay.CreateOrderInput](createOrderStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Razorpay confirms the order."
type completeCreateOrder struct {
	dex.StepDefaultsNoWaitFor[razorpay.CreateOrderResult]
}

func (completeCreateOrder) GetStepType() string { return "CompleteCreateOrder" }
func (completeCreateOrder) Execute(ctx dex.Context, result razorpay.CreateOrderResult) (*dex.StepDecision, error) {
	if err := orderAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateOrderFlow)(nil)
