// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textgeneration runs Zhipu generateText as one Dex Flow.
package textgeneration

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/zhipu/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ZhipuTextGeneration"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "zhipu"
	operationStepType = "CallGenerateText"
)

var textGenerationOutcomeAttribute = dex.DefineAttribute[zhipu.GenerateTextOutput]("zhipu-text-generation-outcome")

// Flow runs GenerateText once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection zhipu.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection zhipu.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{textGenerationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(zhipu.NewGenerateTextStep(zhipu.GenerateTextStepConfig[zhipu.GenerateTextInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Zhipu", Explanation: "Call the provider's generateText operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Generated:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:zhipu-text-generation-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTextGenerationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhipu-text-generation-outcome": outcome}}, nil
}

// dex:field attribute-key:zhipu-text-generation-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTextGenerationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"zhipu-text-generation-outcome": outcome}}, nil
}

func readTextGenerationOutcome(ctx dex.Context) (zhipu.GenerateTextOutput, error) {
	outcome, err := textGenerationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return zhipu.GenerateTextOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input zhipu.GenerateTextInput) zhipu.GenerateTextInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGenerateText" }
func (startRequest) WaitFor(dex.Context, zhipu.GenerateTextInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input zhipu.GenerateTextInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[zhipu.GenerateTextInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[zhipu.GenerateTextResult]
}

func (completeResult) GetStepType() string { return "CompleteGenerateText" }
func (completeResult) Execute(ctx dex.Context, result zhipu.GenerateTextResult) (*dex.StepDecision, error) {
	if err := textGenerationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
