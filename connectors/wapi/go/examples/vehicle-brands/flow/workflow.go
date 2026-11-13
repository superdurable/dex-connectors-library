// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package vehiclebrands runs Wapi listVehicleBrands as one Dex Flow.
package vehiclebrands

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/wapi/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WapiVehicleBrands"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "wapi"
	operationStepType = "CallListVehicleBrands"
)

var brandListOutcomeAttribute = dex.DefineAttribute[wapi.ListVehicleBrandsOutput]("wapi-list-vehicle-brands-outcome")

// Flow runs ListVehicleBrands once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection wapi.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection wapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{brandListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wapi.NewListVehicleBrandsStep(wapi.ListVehicleBrandsStepConfig[wapi.ListVehicleBrandsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wapi", Explanation: "Read vehicle brand IDs and maintenance-record prices for this app."},
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

// dex:field attribute-key:wapi-list-vehicle-brands-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListVehicleBrandsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wapi-list-vehicle-brands-outcome": outcome}}, nil
}

// dex:field attribute-key:wapi-list-vehicle-brands-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListVehicleBrandsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wapi-list-vehicle-brands-outcome": outcome}}, nil
}

func readListVehicleBrandsOutcome(ctx dex.Context) (wapi.ListVehicleBrandsOutput, error) {
	outcome, err := brandListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wapi.ListVehicleBrandsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input wapi.ListVehicleBrandsInput) wapi.ListVehicleBrandsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListVehicleBrands" }
func (startRequest) WaitFor(dex.Context, wapi.ListVehicleBrandsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wapi.ListVehicleBrandsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wapi.ListVehicleBrandsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wapi.ListVehicleBrandsResult]
}

func (completeResult) GetStepType() string { return "CompleteListVehicleBrands" }
func (completeResult) Execute(ctx dex.Context, result wapi.ListVehicleBrandsResult) (*dex.StepDecision, error) {
	if err := brandListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
