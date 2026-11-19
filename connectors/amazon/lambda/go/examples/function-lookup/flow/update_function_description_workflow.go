// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package functionlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/lambda/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// UpdateFunctionDescriptionFlowType identifies the function description update example.
	UpdateFunctionDescriptionFlowType = "AmazonLambdaUpdateFunctionDescription"
	updateFunctionDescriptionStepType = "CallUpdateFunctionDescription"
)

var updateFunctionDescriptionOutcomeAttribute = dex.DefineAttribute[lambda.FunctionDetails]("lambda-function-update-outcome")

// UpdateFunctionDescriptionFlow sets one function description to an absolute value.
type UpdateFunctionDescriptionFlow struct {
	dex.FlowDefaults
	connection lambda.Connection
}

// NewUpdateFunctionDescriptionFlow binds the static Lambda connection at registration.
func NewUpdateFunctionDescriptionFlow(connection lambda.Connection) *UpdateFunctionDescriptionFlow {
	return &UpdateFunctionDescriptionFlow{connection: connection}
}

func (*UpdateFunctionDescriptionFlow) GetFlowType() string { return UpdateFunctionDescriptionFlowType }
func (*UpdateFunctionDescriptionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updateFunctionDescriptionOutcomeAttribute}}
}
func (flow *UpdateFunctionDescriptionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateFunctionDescription{}),
		dex.DefineStep(lambda.NewUpdateFunctionDescriptionStep(lambda.UpdateFunctionDescriptionStepConfig[lambda.UpdateFunctionDescriptionInput]{
			StepType: updateFunctionDescriptionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon Lambda", Explanation: "Set one function description to an absolute value."},
			Connection:  flow.connection, MapToOperationInput: MapToUpdateFunctionDescriptionInput,
			Updated: sdkgo.GoTo(completeUpdateFunctionDescription{}),
		})),
		dex.DefineStep(completeUpdateFunctionDescription{}),
	}
}

// GetRPCs exposes the write outcome in Dex Web summary and display.
func (flow *UpdateFunctionDescriptionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lambda-function-update-outcome value-type:json editable:false description:"Function update outcome"
// GetDexSummary returns the persisted write outcome.
func (*UpdateFunctionDescriptionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUpdateFunctionDescriptionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lambda-function-update-outcome": outcome}}, nil
}

// dex:field attribute-key:lambda-function-update-outcome value-type:json editable:false description:"Function update outcome"
// GetDexDisplay returns the persisted write outcome.
func (*UpdateFunctionDescriptionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUpdateFunctionDescriptionOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lambda-function-update-outcome": outcome}}, nil
}

func readUpdateFunctionDescriptionOutcome(ctx dex.Context) (lambda.FunctionDetails, error) {
	outcome, err := updateFunctionDescriptionOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lambda.FunctionDetails{}, nil
	}
	return outcome, err
}

// MapToUpdateFunctionDescriptionInput preserves the typed write request.
func MapToUpdateFunctionDescriptionInput(input lambda.UpdateFunctionDescriptionInput) lambda.UpdateFunctionDescriptionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the function description to its provider Step."
type startUpdateFunctionDescription struct{ dex.StepDefaults }

func (startUpdateFunctionDescription) GetStepType() string { return "StartUpdateFunctionDescription" }
func (startUpdateFunctionDescription) WaitFor(dex.Context, lambda.UpdateFunctionDescriptionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateFunctionDescription) Execute(_ dex.Context, input lambda.UpdateFunctionDescriptionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lambda.UpdateFunctionDescriptionInput](updateFunctionDescriptionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Lambda accepts the description update."
type completeUpdateFunctionDescription struct {
	dex.StepDefaultsNoWaitFor[lambda.UpdateFunctionDescriptionResult]
}

func (completeUpdateFunctionDescription) GetStepType() string {
	return "CompleteUpdateFunctionDescription"
}
func (completeUpdateFunctionDescription) Execute(ctx dex.Context, result lambda.UpdateFunctionDescriptionResult) (*dex.StepDecision, error) {
	if err := updateFunctionDescriptionOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateFunctionDescriptionFlow)(nil)
