// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package modelcatalog runs Shuntianchuang listModels as one Dex Flow.
package modelcatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/shuntianchuang/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ShuntianchuangModelCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "shuntianchuang"
	operationStepType = "CallListModels"
)

var modelListOutcomeAttribute = dex.DefineAttribute[shuntianchuang.ListModelsOutput]("shuntianchuang-list-models-outcome")

// Flow runs ListModels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection shuntianchuang.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection shuntianchuang.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(shuntianchuang.NewListModelsStep(shuntianchuang.ListModelsStepConfig[shuntianchuang.ListModelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Shuntianchuang", Explanation: "List the models visible to this API Key."},
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

// dex:field attribute-key:shuntianchuang-list-models-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shuntianchuang-list-models-outcome": outcome}}, nil
}

// dex:field attribute-key:shuntianchuang-list-models-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"shuntianchuang-list-models-outcome": outcome}}, nil
}

func readListModelsOutcome(ctx dex.Context) (shuntianchuang.ListModelsOutput, error) {
	outcome, err := modelListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return shuntianchuang.ListModelsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input shuntianchuang.ListModelsInput) shuntianchuang.ListModelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListModels" }
func (startRequest) WaitFor(dex.Context, shuntianchuang.ListModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input shuntianchuang.ListModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[shuntianchuang.ListModelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[shuntianchuang.ListModelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListModels" }
func (completeResult) Execute(ctx dex.Context, result shuntianchuang.ListModelsResult) (*dex.StepDecision, error) {
	if err := modelListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
