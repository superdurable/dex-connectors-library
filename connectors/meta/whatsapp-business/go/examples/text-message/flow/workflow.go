// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package textmessage runs WhatsApp Business sendTextMessage as one Dex Flow.
package textmessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WhatsAppBusinessSendTextMessage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "whatsapp-business"
	operationStepType = "CallSendTextMessage"
)

var sentMessageAttribute = dex.DefineAttribute[whatsappbusiness.SentMessage]("whatsapp-business-text-message-sent-message")

// Flow runs SendTextMessage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection whatsappbusiness.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection whatsappbusiness.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentMessageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(whatsappbusiness.NewSendTextMessageStep(whatsappbusiness.SendTextMessageStepConfig[whatsappbusiness.SendTextMessageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WhatsApp Business", Explanation: "Call the provider's sendTextMessage operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Sent:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:whatsapp-business-text-message-sent-message value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-text-message-sent-message": outcome}}, nil
}

// dex:field attribute-key:whatsapp-business-text-message-sent-message value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-text-message-sent-message": outcome}}, nil
}

func readSentMessage(ctx dex.Context) (whatsappbusiness.SentMessage, error) {
	outcome, err := sentMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return whatsappbusiness.SentMessage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input whatsappbusiness.SendTextMessageInput) whatsappbusiness.SendTextMessageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSendTextMessage" }
func (startRequest) WaitFor(dex.Context, whatsappbusiness.SendTextMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input whatsappbusiness.SendTextMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[whatsappbusiness.SendTextMessageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[whatsappbusiness.SendTextMessageResult]
}

func (completeResult) GetStepType() string { return "CompleteSendTextMessage" }
func (completeResult) Execute(ctx dex.Context, result whatsappbusiness.SendTextMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
