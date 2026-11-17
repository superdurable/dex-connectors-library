// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sendmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const sendPhotoStepType = "CallSendPhoto"

var sentPhotoAttribute = dex.DefineAttribute[telegram.SentMessage]("telegram-sent-photo")

// SendPhotoFlow sends a photo to a bot-accessible Telegram chat.
type SendPhotoFlow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewSendPhotoFlow binds the connection at Worker registration.
func NewSendPhotoFlow(connection telegram.Connection) *SendPhotoFlow {
	return &SendPhotoFlow{connection: connection}
}

// GetFlowType returns the stable photo-send Flow identity.
func (*SendPhotoFlow) GetFlowType() string { return "TelegramSendPhoto" }

// GetPersistenceSchema registers the sent photo identity.
func (*SendPhotoFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentPhotoAttribute}}
}

// GetSteps registers the provider mutation and completion Steps.
func (flow *SendPhotoFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginSendPhoto{}),
		dex.DefineStep(telegram.NewSendPhotoStep(telegram.SendPhotoStepConfig[telegram.SendPhotoInput]{
			StepType: sendPhotoStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Send a photo to the selected chat."},
			MapToOperationInput: mapSendPhotoInput, Sent: sdkgo.GoTo(completeSendPhoto{}),
		})),
		dex.DefineStep(completeSendPhoto{}),
	}
}

// GetRPCs exposes sent photo identity in Dex Web.
func (flow *SendPhotoFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the sent photo identity when available.
// dex:field attribute-key:telegram-sent-photo value-type:json editable:false description:"Sent photo"
func (*SendPhotoFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentPhoto(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-sent-photo": message}}, nil
}

// GetDexDisplay returns the sent photo identity when available.
// dex:field attribute-key:telegram-sent-photo value-type:json editable:false description:"Sent photo"
func (*SendPhotoFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentPhoto(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-sent-photo": message}}, nil
}

func readSentPhoto(ctx dex.Context) (telegram.SentMessage, error) {
	message, err := sentPhotoAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.SentMessage{}, nil
	}
	return message, err
}

func mapSendPhotoInput(input telegram.SendPhotoInput) telegram.SendPhotoInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the photo reference and destination chat to Telegram."
type beginSendPhoto struct{ dex.StepDefaults }

func (beginSendPhoto) WaitFor(dex.Context, telegram.SendPhotoInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginSendPhoto) Execute(_ dex.Context, input telegram.SendPhotoInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.SendPhotoInput](sendPhotoStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow after Telegram confirms the send."
type completeSendPhoto struct {
	dex.StepDefaultsNoWaitFor[telegram.SendPhotoResult]
}

func (completeSendPhoto) Execute(ctx dex.Context, result telegram.SendPhotoResult) (*dex.StepDecision, error) {
	if err := sentPhotoAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendPhotoFlow)(nil)
