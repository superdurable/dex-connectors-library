// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// OrderFlowType identifies the order lookup example Flow.
const OrderFlowType = "KeapOrderLookup"

const getOrderStepType = "CallGetOrder"

var orderProfileAttribute = dex.DefineAttribute[keap.OrderProfile]("keap-order-profile")

// OrderFlow reads one Keap order.
type OrderFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewOrderFlow binds the static Keap connection at registration.
func NewOrderFlow(connection keap.Connection) *OrderFlow { return &OrderFlow{connection: connection} }

func (*OrderFlow) GetFlowType() string { return OrderFlowType }

func (*OrderFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{orderProfileAttribute}}
}

func (flow *OrderFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startOrderLookup{}),
		dex.DefineStep(keap.NewGetOrderStep(keap.GetOrderStepConfig[keap.OrderIDInput]{
			StepType: getOrderStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "Read one CRM order."},
			Connection:  flow.connection, MapToOperationInput: mapOrderID,
			Found: sdkgo.GoTo(completeOrderLookup{}),
		})),
		dex.DefineStep(completeOrderLookup{}),
	}
}

func (flow *OrderFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-order-profile value-type:json editable:false description:"Order profile"
// GetDexSummary shows the order in Dex Web.
func (*OrderFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := readOrderProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-order-profile": order}}, nil
}

// dex:field attribute-key:keap-order-profile value-type:json editable:false description:"Order profile"
// GetDexDisplay shows the order in Dex Web.
func (*OrderFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	order, err := readOrderProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-order-profile": order}}, nil
}

func readOrderProfile(ctx dex.Context) (keap.OrderProfile, error) {
	order, err := orderProfileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.OrderProfile{}, nil
	}
	return order, err
}

func mapOrderID(input keap.OrderIDInput) keap.OrderIDInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected order ID to the provider Step."
type startOrderLookup struct{ dex.StepDefaults }

func (startOrderLookup) GetStepType() string { return "StartOrderLookup" }
func (startOrderLookup) WaitFor(dex.Context, keap.OrderIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startOrderLookup) Execute(_ dex.Context, input keap.OrderIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.OrderIDInput](getOrderStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the returned order and complete the Flow."
type completeOrderLookup struct {
	dex.StepDefaultsNoWaitFor[keap.GetOrderResult]
}

func (completeOrderLookup) GetStepType() string { return "CompleteOrderLookup" }
func (completeOrderLookup) Execute(ctx dex.Context, result keap.GetOrderResult) (*dex.StepDecision, error) {
	if err := orderProfileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*OrderFlow)(nil)
