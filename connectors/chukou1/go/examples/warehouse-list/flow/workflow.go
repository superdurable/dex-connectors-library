// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package warehouselist runs Chukou1 listWarehouses as one Dex Flow.
package warehouselist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/chukou1/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "Chukou1WarehouseList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "chukou1"
	operationStepType = "CallListWarehouses"
)

var warehouseListOutcomeAttribute = dex.DefineAttribute[chukou1.WarehousePage]("chukou1-warehouse-list-outcome")

// Flow runs ListWarehouses once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection chukou1.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection chukou1.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{warehouseListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(chukou1.NewListWarehousesStep(chukou1.ListWarehousesStepConfig[chukou1.ListWarehousesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Chukou1", Explanation: "List Chukou1 overseas warehouses."},
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

// dex:field attribute-key:chukou1-warehouse-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readWarehousePageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chukou1-warehouse-list-outcome": outcome}}, nil
}

// dex:field attribute-key:chukou1-warehouse-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readWarehousePageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chukou1-warehouse-list-outcome": outcome}}, nil
}

func readWarehousePageOutcome(ctx dex.Context) (chukou1.WarehousePage, error) {
	outcome, err := warehouseListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return chukou1.WarehousePage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input chukou1.ListWarehousesInput) chukou1.ListWarehousesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListWarehouses" }
func (startRequest) WaitFor(dex.Context, chukou1.ListWarehousesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input chukou1.ListWarehousesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[chukou1.ListWarehousesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[chukou1.ListWarehousesResult]
}

func (completeResult) GetStepType() string { return "CompleteListWarehouses" }
func (completeResult) Execute(ctx dex.Context, result chukou1.ListWarehousesResult) (*dex.StepDecision, error) {
	if err := warehouseListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
