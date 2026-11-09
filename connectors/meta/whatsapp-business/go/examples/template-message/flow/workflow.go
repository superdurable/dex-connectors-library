// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package templatemessage runs WhatsApp Business sendTemplateMessage as one Dex Flow.
package templatemessage

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/whatsapp-business/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WhatsAppBusinessSendTemplateMessage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "whatsapp-business"
	operationStepType = "CallSendTemplateMessage"
)

var sentMessageAttribute = dex.DefineAttribute[whatsappbusiness.SentMessage]("whatsapp-business-template-message-sent-message")

// Flow runs SendTemplateMessage once and completes with its typed output.
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
		dex.DefineStep(whatsappbusiness.NewSendTemplateMessageStep(whatsappbusiness.SendTemplateMessageStepConfig[whatsappbusiness.SendTemplateMessageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WhatsApp Business", Explanation: "Call the provider's sendTemplateMessage operation."},
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

// dex:field attribute-key:whatsapp-business-template-message-sent-message value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-template-message-sent-message": outcome}}, nil
}

// dex:field attribute-key:whatsapp-business-template-message-sent-message value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"whatsapp-business-template-message-sent-message": outcome}}, nil
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
func MapToOperationInput(input whatsappbusiness.SendTemplateMessageInput) whatsappbusiness.SendTemplateMessageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSendTemplateMessage" }
func (startRequest) WaitFor(dex.Context, whatsappbusiness.SendTemplateMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input whatsappbusiness.SendTemplateMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[whatsappbusiness.SendTemplateMessageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[whatsappbusiness.SendTemplateMessageResult]
}

func (completeResult) GetStepType() string { return "CompleteSendTemplateMessage" }
func (completeResult) Execute(ctx dex.Context, result whatsappbusiness.SendTemplateMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
