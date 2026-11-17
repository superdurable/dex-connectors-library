// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package chatlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const setChatTitleStepType = "CallSetChatTitle"

var titledChatAttribute = dex.DefineAttribute[telegram.ChatActionResult]("telegram-titled-chat")

// SetChatTitleFlow replaces a Telegram chat title through the static connection.
type SetChatTitleFlow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewSetChatTitleFlow binds the connection at Worker registration.
func NewSetChatTitleFlow(connection telegram.Connection) *SetChatTitleFlow {
	return &SetChatTitleFlow{connection: connection}
}

// GetFlowType returns the stable title-update Flow identity.
func (*SetChatTitleFlow) GetFlowType() string { return "TelegramSetChatTitle" }

// GetPersistenceSchema registers the confirmed chat identity.
func (*SetChatTitleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{titledChatAttribute}}
}

// GetSteps registers the provider mutation and completion Steps.
func (flow *SetChatTitleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginSetChatTitle{}),
		dex.DefineStep(telegram.NewSetChatTitleStep(telegram.SetChatTitleStepConfig[telegram.SetChatTitleInput]{
			StepType: setChatTitleStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Replace the chat title."},
			MapToOperationInput: mapSetChatTitleInput, Updated: sdkgo.GoTo(completeSetChatTitle{}),
		})),
		dex.DefineStep(completeSetChatTitle{}),
	}
}

// GetRPCs exposes the confirmed chat ID in Dex Web.
func (flow *SetChatTitleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the confirmed chat ID when available.
// dex:field attribute-key:telegram-titled-chat value-type:json editable:false description:"Titled chat"
func (*SetChatTitleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readTitledChat(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-titled-chat": chat}}, nil
}

// GetDexDisplay returns the confirmed chat ID when available.
// dex:field attribute-key:telegram-titled-chat value-type:json editable:false description:"Titled chat"
func (*SetChatTitleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readTitledChat(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-titled-chat": chat}}, nil
}

func readTitledChat(ctx dex.Context) (telegram.ChatActionResult, error) {
	chat, err := titledChatAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.ChatActionResult{}, nil
	}
	return chat, err
}

func mapSetChatTitleInput(input telegram.SetChatTitleInput) telegram.SetChatTitleInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the chat ID and replacement title to Telegram."
type beginSetChatTitle struct{ dex.StepDefaults }

func (beginSetChatTitle) WaitFor(dex.Context, telegram.SetChatTitleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginSetChatTitle) Execute(_ dex.Context, input telegram.SetChatTitleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.SetChatTitleInput](setChatTitleStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow after Telegram confirms the title update."
type completeSetChatTitle struct {
	dex.StepDefaultsNoWaitFor[telegram.SetChatTitleResult]
}

func (completeSetChatTitle) Execute(ctx dex.Context, result telegram.SetChatTitleResult) (*dex.StepDecision, error) {
	if err := titledChatAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SetChatTitleFlow)(nil)
