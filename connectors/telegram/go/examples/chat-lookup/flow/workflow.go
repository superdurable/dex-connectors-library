// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package chatlookup runs a Telegram chat query as a Dex Flow.
package chatlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/telegram/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TelegramChatLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "telegram"
	operationStepType = "CallGetChat"
)

var inspectedChatAttribute = dex.DefineAttribute[telegram.Chat]("telegram-inspected-chat")

// Flow reads one chat metadata and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection telegram.Connection
}

// NewFlow binds the Telegram connection at registration time.
func NewFlow(connection telegram.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedChatAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(telegram.NewGetChatStep(telegram.GetChatStepConfig[telegram.GetChatInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Telegram", Explanation: "Read the chat metadata by ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:telegram-inspected-chat value-type:json editable:false description:"Chat metadata"
// GetDexSummary shows the inspected chat in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readInspectedChat(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-inspected-chat": chat}}, nil
}

// dex:field attribute-key:telegram-inspected-chat value-type:json editable:false description:"Chat metadata"
// GetDexDisplay shows the inspected chat in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	chat, err := readInspectedChat(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"telegram-inspected-chat": chat}}, nil
}

func readInspectedChat(ctx dex.Context) (telegram.Chat, error) {
	chat, err := inspectedChatAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return telegram.Chat{}, nil
	}
	return chat, err
}

func mapToOperationInput(input telegram.GetChatInput) telegram.GetChatInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the chat ID to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetChat" }
func (startRequest) WaitFor(dex.Context, telegram.GetChatInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input telegram.GetChatInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[telegram.GetChatInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the chat metadata."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[telegram.GetChatResult]
}

func (completeResult) GetStepType() string { return "CompleteGetChat" }
func (completeResult) Execute(ctx dex.Context, result telegram.GetChatResult) (*dex.StepDecision, error) {
	if err := inspectedChatAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
