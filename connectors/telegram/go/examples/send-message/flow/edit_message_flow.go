// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const editMessageStepType = "CallEditMessageText"

var editedMessageAttribute = dex.DefineAttribute[telegram.MessageActionResult]("telegram-edited-message")

// EditMessageFlow replaces plain text on a bot-editable Telegram message.
type EditMessageFlow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewEditMessageFlow binds the connection at Worker registration.
func NewEditMessageFlow(connection telegram.Connection) *EditMessageFlow {
	return &EditMessageFlow{connection: connection}
}

// GetFlowType returns the stable message-edit Flow identity.
func (*EditMessageFlow) GetFlowType() string { return "TelegramEditMessageText" }

// GetPersistenceSchema registers the edited message identity.
func (*EditMessageFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{editedMessageAttribute}}
}

// GetSteps registers the provider mutation and completion Steps.
func (flow *EditMessageFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginEditMessage{}),
		dex.DefineStep(telegram.NewEditMessageTextStep(telegram.EditMessageTextStepConfig[telegram.EditMessageTextInput]{
			StepType: editMessageStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Replace a message's plain text."},
			MapToOperationInput: mapEditMessageInput, Edited: sdkgo.GoTo(completeEditMessage{}),
		})),
		dex.DefineStep(completeEditMessage{}),
	}
}

// GetRPCs exposes edited message identity in Dex Web.
func (flow *EditMessageFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the edited message identity when available.
// dex:field attribute-key:telegram-edited-message value-type:json editable:false description:"Edited message"
func (*EditMessageFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readEditedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-edited-message": message}}, nil
}

// GetDexDisplay returns the edited message identity when available.
// dex:field attribute-key:telegram-edited-message value-type:json editable:false description:"Edited message"
func (*EditMessageFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readEditedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-edited-message": message}}, nil
}

func readEditedMessage(ctx dex.Context) (telegram.MessageActionResult, error) {
	message, err := editedMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.MessageActionResult{}, nil
	}
	return message, err
}

func mapEditMessageInput(input telegram.EditMessageTextInput) telegram.EditMessageTextInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the message identity and replacement text to Telegram."
type beginEditMessage struct{ dex.StepDefaults }

func (beginEditMessage) WaitFor(dex.Context, telegram.EditMessageTextInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginEditMessage) Execute(_ dex.Context, input telegram.EditMessageTextInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.EditMessageTextInput](editMessageStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow after Telegram confirms the edit."
type completeEditMessage struct {
	dex.StepDefaultsNoWaitFor[telegram.EditMessageTextResult]
}

func (completeEditMessage) Execute(ctx dex.Context, result telegram.EditMessageTextResult) (*dex.StepDecision, error) {
	if err := editedMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*EditMessageFlow)(nil)
