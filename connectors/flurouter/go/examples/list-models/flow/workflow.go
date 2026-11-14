// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package models runs FluRouter listModels as one Dex Flow.
package models

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/flurouter/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FluRouterModels"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "flurouter"
	operationStepType = "CallListModels"
)

var modelsOutcomeAttribute = dex.DefineAttribute[flurouter.ListModelsOutput]("flurouter-list-models-outcome")

// Flow runs ListModels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection flurouter.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection flurouter.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(flurouter.NewListModelsStep(flurouter.ListModelsStepConfig[flurouter.ListModelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FluRouter", Explanation: "List model IDs visible to the connected Key."},
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

// dex:field attribute-key:flurouter-list-models-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"flurouter-list-models-outcome": outcome}}, nil
}

// dex:field attribute-key:flurouter-list-models-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"flurouter-list-models-outcome": outcome}}, nil
}

func readListModelsOutcome(ctx dex.Context) (flurouter.ListModelsOutput, error) {
	outcome, err := modelsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return flurouter.ListModelsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input flurouter.ListModelsInput) flurouter.ListModelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListModels" }
func (startRequest) WaitFor(dex.Context, flurouter.ListModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input flurouter.ListModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[flurouter.ListModelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[flurouter.ListModelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListModels" }
func (completeResult) Execute(ctx dex.Context, result flurouter.ListModelsResult) (*dex.StepDecision, error) {
	if err := modelsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
