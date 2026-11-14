// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package orderstatus runs Dada queryOrderStatus as one Dex Flow.
package orderstatus

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/dada/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DadaOrderStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dada"
	operationStepType = "CallQueryOrderStatus"
)

var statusAttribute = dex.DefineAttribute[dada.OrderStatus]("dada-order-status")

// Flow runs QueryOrderStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dada.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dada.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{statusAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dada.NewQueryOrderStatusStep(dada.QueryOrderStatusStepConfig[dada.QueryOrderStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dada", Explanation: "Read the authorized Dada order status."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dada-order-status value-type:json editable:false description:"Dada order status information"
// GetDexSummary shows the last Dada order status information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dada-order-status": outcome}}, nil
}

// dex:field attribute-key:dada-order-status value-type:json editable:false description:"Dada order status information"
// GetDexDisplay shows the last Dada order status information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dada-order-status": outcome}}, nil
}

func readStatus(ctx dex.Context) (dada.OrderStatus, error) {
	outcome, err := statusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dada.OrderStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input dada.QueryOrderStatusInput) dada.QueryOrderStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryOrderStatus" }
func (startRequest) WaitFor(dex.Context, dada.QueryOrderStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dada.QueryOrderStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dada.QueryOrderStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dada.QueryOrderStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryOrderStatus" }
func (completeResult) Execute(ctx dex.Context, result dada.QueryOrderStatusResult) (*dex.StepDecision, error) {
	if err := statusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
