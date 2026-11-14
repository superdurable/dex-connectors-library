// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package conversationread runs Meiqia getConversation as one Dex Flow.
package conversationread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meiqia/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MeiqiaConversationRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "meiqia"
	operationStepType = "CallGetConversation"
)

var conversationOutcomeAttribute = dex.DefineAttribute[meiqia.GetConversationOutput]("meiqia-get-conversation-outcome")

// Flow runs GetConversation once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection meiqia.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection meiqia.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{conversationOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(meiqia.NewGetConversationStep(meiqia.GetConversationStepConfig[meiqia.GetConversationInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Meiqia", Explanation: "Read one Meiqia conversation without message or customer content."},
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

// dex:field attribute-key:meiqia-get-conversation-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetConversationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meiqia-get-conversation-outcome": outcome}}, nil
}

// dex:field attribute-key:meiqia-get-conversation-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetConversationOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meiqia-get-conversation-outcome": outcome}}, nil
}

func readGetConversationOutcome(ctx dex.Context) (meiqia.GetConversationOutput, error) {
	outcome, err := conversationOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meiqia.GetConversationOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input meiqia.GetConversationInput) meiqia.GetConversationInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the conversation ID to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetConversation" }
func (startRequest) WaitFor(dex.Context, meiqia.GetConversationInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input meiqia.GetConversationInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meiqia.GetConversationInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the conversation metadata."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[meiqia.GetConversationResult]
}

func (completeResult) GetStepType() string { return "CompleteGetConversation" }
func (completeResult) Execute(ctx dex.Context, result meiqia.GetConversationResult) (*dex.StepDecision, error) {
	if err := conversationOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
