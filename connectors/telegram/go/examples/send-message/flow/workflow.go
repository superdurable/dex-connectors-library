// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sendmessage sends one Telegram text message as a Dex Flow.
package sendmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TelegramMessageSend"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "telegram"
	operationStepType = "CallSendMessage"
)

var sentMessageAttribute = dex.DefineAttribute[telegram.SentMessage]("telegram-sent-message")

// Flow sends one message and completes with its provider identity.
type Flow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewFlow binds the Telegram connection at registration time.
func NewFlow(connection telegram.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentMessageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(telegram.NewSendMessageStep(telegram.SendMessageStepConfig[telegram.SendMessageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Send one text message to the selected chat."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Sent:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:telegram-sent-message value-type:json editable:false description:"Sent message"
// GetDexSummary shows the sent message identity in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-sent-message": chat}}, nil
}

// dex:field attribute-key:telegram-sent-message value-type:json editable:false description:"Sent message"
// GetDexDisplay shows the sent message identity in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-sent-message": chat}}, nil
}

func readSentMessage(ctx dex.Context) (telegram.SentMessage, error) {
	chat, err := sentMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.SentMessage{}, nil
	}
	return chat, err
}

func mapToOperationInput(input telegram.SendMessageInput) telegram.SendMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the message input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSendMessage" }
func (startRequest) WaitFor(dex.Context, telegram.SendMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input telegram.SendMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.SendMessageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the sent message."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[telegram.SendMessageResult]
}

func (completeResult) GetStepType() string { return "CompleteSendMessage" }
func (completeResult) Execute(ctx dex.Context, result telegram.SendMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
