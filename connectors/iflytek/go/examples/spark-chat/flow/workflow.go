// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sparkchat runs IFLYTEK createChatCompletion as one Dex Flow.
package sparkchat

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/iflytek/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "IFLYTEKChatCompletion"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "iflytek"
	operationStepType = "CallCreateChatCompletion"
)

var completionAttribute = dex.DefineAttribute[iflytek.ChatCompletion]("iflytek-spark-chat")

// Flow runs CreateChatCompletion once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection iflytek.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection iflytek.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Spark chat Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the completion attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{completionAttribute}}
}

// GetSteps wires the request, provider query, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(iflytek.NewCreateChatCompletionStep(iflytek.CreateChatCompletionStepConfig[iflytek.CreateChatCompletionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "IFLYTEK", Explanation: "Generate a Spark assistant answer."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Completed:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:iflytek-spark-chat value-type:json editable:false description:"Spark assistant answer"
// GetDexSummary shows the last Spark assistant answer in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCompletion(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"iflytek-spark-chat": outcome}}, nil
}

// dex:field attribute-key:iflytek-spark-chat value-type:json editable:false description:"Spark assistant answer"
// GetDexDisplay shows the last Spark assistant answer in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCompletion(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"iflytek-spark-chat": outcome}}, nil
}

func readCompletion(ctx dex.Context) (iflytek.ChatCompletion, error) {
	outcome, err := completionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return iflytek.ChatCompletion{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input iflytek.CreateChatCompletionInput) iflytek.CreateChatCompletionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCreateChatCompletion" }
func (startRequest) WaitFor(dex.Context, iflytek.CreateChatCompletionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input iflytek.CreateChatCompletionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[iflytek.CreateChatCompletionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[iflytek.CreateChatCompletionResult]
}

func (completeResult) GetStepType() string { return "CompleteCreateChatCompletion" }
func (completeResult) Execute(ctx dex.Context, result iflytek.CreateChatCompletionResult) (*dex.StepDecision, error) {
	if err := completionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
