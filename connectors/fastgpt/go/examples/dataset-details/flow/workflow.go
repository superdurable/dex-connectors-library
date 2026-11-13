// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package datasetdetails runs FastGPT getDatasetDetails as one Dex Flow.
package datasetdetails

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/fastgpt/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FastGPTDatasetDetails"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fastgpt"
	operationStepType = "CallGetDatasetDetails"
)

var datasetDetailsOutcomeAttribute = dex.DefineAttribute[fastgpt.GetDatasetDetailsOutput]("fastgpt-dataset-details-outcome")

// Flow runs GetDatasetDetails once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fastgpt.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fastgpt.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{datasetDetailsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fastgpt.NewGetDatasetDetailsStep(fastgpt.GetDatasetDetailsStepConfig[fastgpt.GetDatasetDetailsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FastGPT", Explanation: "Read one knowledge base ID, name, type, and status without content."},
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

// dex:field attribute-key:fastgpt-dataset-details-outcome value-type:json editable:false description:"Dataset catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDatasetDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fastgpt-dataset-details-outcome": outcome}}, nil
}

// dex:field attribute-key:fastgpt-dataset-details-outcome value-type:json editable:false description:"Dataset catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDatasetDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fastgpt-dataset-details-outcome": outcome}}, nil
}

func readDatasetDetailsOutcome(ctx dex.Context) (fastgpt.GetDatasetDetailsOutput, error) {
	outcome, err := datasetDetailsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fastgpt.GetDatasetDetailsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fastgpt.GetDatasetDetailsInput) fastgpt.GetDatasetDetailsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetDatasetDetails" }
func (startRequest) WaitFor(dex.Context, fastgpt.GetDatasetDetailsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fastgpt.GetDatasetDetailsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fastgpt.GetDatasetDetailsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fastgpt.GetDatasetDetailsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetDatasetDetails" }
func (completeResult) Execute(ctx dex.Context, result fastgpt.GetDatasetDetailsResult) (*dex.StepDecision, error) {
	if err := datasetDetailsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
