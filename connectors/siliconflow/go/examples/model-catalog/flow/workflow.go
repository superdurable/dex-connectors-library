// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package modelcatalog runs SiliconFlow listModels as one Dex Flow.
package modelcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/siliconflow/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SiliconFlowModelCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "siliconflow"
	operationStepType = "CallListModels"
)

var modelCatalogOutcomeAttribute = dex.DefineAttribute[siliconflow.ListModelsOutput]("siliconflow-model-catalog-outcome")

// Flow runs ListModels once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection siliconflow.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection siliconflow.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(siliconflow.NewListModelsStep(siliconflow.ListModelsStepConfig[siliconflow.ListModelsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SiliconFlow", Explanation: "Call the provider's listModels operation."},
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

// dex:field attribute-key:siliconflow-model-catalog-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"siliconflow-model-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:siliconflow-model-catalog-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"siliconflow-model-catalog-outcome": outcome}}, nil
}

func readModelCatalogOutcome(ctx dex.Context) (siliconflow.ListModelsOutput, error) {
	outcome, err := modelCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return siliconflow.ListModelsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the empty start input to the provider call.
func MapToOperationInput(input siliconflow.ListModelsInput) siliconflow.ListModelsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListModels" }
func (startRequest) WaitFor(dex.Context, siliconflow.ListModelsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input siliconflow.ListModelsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[siliconflow.ListModelsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[siliconflow.ListModelsResult]
}

func (completeResult) GetStepType() string { return "CompleteListModels" }
func (completeResult) Execute(ctx dex.Context, result siliconflow.ListModelsResult) (*dex.StepDecision, error) {
	if err := modelCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
