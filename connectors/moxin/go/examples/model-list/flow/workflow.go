// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package modellist runs Moxin listModels as one Dex Flow.
package modellist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/moxin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MoxinModels"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "moxin"
	operationStepType = "CallListModels"
)

var modelListOutcomeAttribute = dex.DefineAttribute[moxin.ModelPage]("moxin-model-list-outcome")

// Flow runs ListModels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection moxin.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection moxin.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(moxin.NewListModelsStep(moxin.ListModelsStepConfig[moxin.ListModelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Moxin", Explanation: "List Moxin API model balances."},
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

// dex:field attribute-key:moxin-model-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"moxin-model-list-outcome": outcome}}, nil
}

// dex:field attribute-key:moxin-model-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"moxin-model-list-outcome": outcome}}, nil
}

func readModelsOutcome(ctx dex.Context) (moxin.ModelPage, error) {
	outcome, err := modelListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return moxin.ModelPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input moxin.ListModelsInput) moxin.ListModelsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListModels" }
func (startRequest) WaitFor(dex.Context, moxin.ListModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input moxin.ListModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[moxin.ListModelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[moxin.ListModelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListModels" }
func (completeResult) Execute(ctx dex.Context, result moxin.ListModelsResult) (*dex.StepDecision, error) {
	if err := modelListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
