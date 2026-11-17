// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// MessageCreateFlowType identifies the message creation example.
const MessageCreateFlowType = "BasecampMessageCreate"

const messageCreateStepType = "CallCreateMessage"

var createdMessageAttribute = dex.DefineAttribute[basecamp.Message]("basecamp-created-message")

// MessageCreateFlow creates one Basecamp message.
type MessageCreateFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewMessageCreateFlow binds the provider connection at registration.
func NewMessageCreateFlow(connection basecamp.Connection) *MessageCreateFlow {
	return &MessageCreateFlow{connection: connection}
}

func (*MessageCreateFlow) GetFlowType() string { return MessageCreateFlowType }

func (*MessageCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdMessageAttribute}}
}

// GetRPCs registers read-only message views.
func (flow *MessageCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-created-message value-type:json editable:false description:"Created message"
// GetDexSummary shows the created message identity.
func (*MessageCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readCreatedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-message": message}}, nil
}

// dex:field attribute-key:basecamp-created-message value-type:json editable:false description:"Created message"
// GetDexDisplay shows the created message identity.
func (*MessageCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readCreatedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-message": message}}, nil
}

func readCreatedMessage(ctx dex.Context) (basecamp.Message, error) {
	message, err := createdMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.Message{}, nil
	}
	return message, err
}

func (flow *MessageCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMessageCreate{}),
		dex.DefineStep(basecamp.NewCreateMessageStep(basecamp.CreateMessageStepConfig[basecamp.CreateMessageInput]{
			StepType: messageCreateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Create one message in the selected message board."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.CreateMessageInput) basecamp.CreateMessageInput { return input },
			Created:             sdkgo.GoTo(completeMessageCreate{}),
		})),
		dex.DefineStep(completeMessageCreate{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the message fields to the creation Step."
type startMessageCreate struct{ dex.StepDefaults }

func (startMessageCreate) GetStepType() string { return "StartCreateMessage" }
func (startMessageCreate) WaitFor(dex.Context, basecamp.CreateMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMessageCreate) Execute(_ dex.Context, input basecamp.CreateMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.CreateMessageInput](messageCreateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created message identity."
type completeMessageCreate struct {
	dex.StepDefaultsNoWaitFor[basecamp.CreateMessageResult]
}

func (completeMessageCreate) GetStepType() string { return "CompleteCreateMessage" }
func (completeMessageCreate) Execute(ctx dex.Context, result basecamp.CreateMessageResult) (*dex.StepDecision, error) {
	if err := createdMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MessageCreateFlow)(nil)
