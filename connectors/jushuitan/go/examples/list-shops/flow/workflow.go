// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package listshops runs Jushuitan listShops as one Dex Flow.
package listshops

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/jushuitan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JushuitanShopList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jushuitan"
	operationStepType = "CallListShops"
)

var shopsOutcomeAttribute = dex.DefineAttribute[jushuitan.ListShopsOutput]("jushuitan-list-shops-outcome")

// Flow runs ListShops once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jushuitan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jushuitan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{shopsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jushuitan.NewListShopsStep(jushuitan.ListShopsStepConfig[jushuitan.ListShopsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jushuitan", Explanation: "Call the provider's listShops operation."},
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

// dex:field attribute-key:jushuitan-list-shops-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListShopsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jushuitan-list-shops-outcome": outcome}}, nil
}

// dex:field attribute-key:jushuitan-list-shops-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListShopsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jushuitan-list-shops-outcome": outcome}}, nil
}

func readListShopsOutcome(ctx dex.Context) (jushuitan.ListShopsOutput, error) {
	outcome, err := shopsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jushuitan.ListShopsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jushuitan.ListShopsInput) jushuitan.ListShopsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListShops" }
func (startRequest) WaitFor(dex.Context, jushuitan.ListShopsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jushuitan.ListShopsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jushuitan.ListShopsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jushuitan.ListShopsResult]
}

func (completeResult) GetStepType() string { return "CompleteListShops" }
func (completeResult) Execute(ctx dex.Context, result jushuitan.ListShopsResult) (*dex.StepDecision, error) {
	if err := shopsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
