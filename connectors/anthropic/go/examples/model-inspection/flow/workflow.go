// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package modelinspection runs Anthropic getModel as one Dex Flow.
package modelinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/anthropic/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AnthropicModelInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "anthropic"
	operationStepType = "InspectModel"
)

var modelinspectionOutcomeAttribute = dex.DefineAttribute[anthropic.ModelInfo]("anthropic-modelinspection-outcome")

// Flow runs GetModel once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection anthropic.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection anthropic.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{modelinspectionOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(anthropic.NewGetModelStep(anthropic.GetModelStepConfig[anthropic.GetModelInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Anthropic", Explanation: "Call the provider's getModel operation."},
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

// dex:field attribute-key:anthropic-modelinspection-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelInspectionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"anthropic-modelinspection-outcome": outcome}}, nil
}

// dex:field attribute-key:anthropic-modelinspection-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readModelInspectionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"anthropic-modelinspection-outcome": outcome}}, nil
}

func readModelInspectionOutcome(ctx dex.Context) (anthropic.ModelInfo, error) {
	outcome, err := modelinspectionOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return anthropic.ModelInfo{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input anthropic.GetModelInput) anthropic.GetModelInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetModel" }
func (startRequest) WaitFor(dex.Context, anthropic.GetModelInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input anthropic.GetModelInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[anthropic.GetModelInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[anthropic.GetModelResult]
}

func (completeResult) GetStepType() string { return "CompleteGetModel" }
func (completeResult) Execute(ctx dex.Context, result anthropic.GetModelResult) (*dex.StepDecision, error) {
	if err := modelinspectionOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
