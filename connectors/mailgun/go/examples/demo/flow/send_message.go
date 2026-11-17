// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/mailgun/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SendFlow queues one email through the static Mailgun connection.
type SendFlow struct {
	dex.FlowDefaults
	connection mailgun.Connection
}

// NewSendFlow binds the provider connection at registration time.
func NewSendFlow(connection mailgun.Connection) *SendFlow { return &SendFlow{connection: connection} }

func (*SendFlow) GetFlowType() string { return "MailgunSendMessage" }

var sentMessageAttribute = dex.DefineAttribute[mailgun.SentMessage]("mailgun-sent-message")

func (*SendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentMessageAttribute}}
}

func (flow *SendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSendMessage{}),
		dex.DefineStep(mailgun.NewSendMessageStep(mailgun.SendMessageStepConfig[mailgun.SendMessageInput]{
			StepType: "CallSendMessage", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Mailgun", Explanation: "Queue one email through the verified Mailgun sending domain."},
			Connection:  flow.connection, MapToOperationInput: mapSendMessageInput,
			Sent: sdkgo.GoTo(completeSendMessage{}),
		})),
		dex.DefineStep(completeSendMessage{}),
	}
}

func (flow *SendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:mailgun-sent-message value-type:json editable:false description:"Queued message ID"
// GetDexSummary shows the last queued message ID in Dex Web.
func (*SendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mailgun-sent-message": value}}, nil
}

// dex:field attribute-key:mailgun-sent-message value-type:json editable:false description:"Queued message ID"
// GetDexDisplay shows the last queued message ID in Dex Web.
func (*SendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mailgun-sent-message": value}}, nil
}

func readSentMessage(ctx dex.Context) (mailgun.SentMessage, error) {
	value, err := sentMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return mailgun.SentMessage{}, nil
	}
	return value, err
}

func mapSendMessageInput(input mailgun.SendMessageInput) mailgun.SendMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the verified domain and email content to Mailgun."
type startSendMessage struct{ dex.StepDefaults }

func (startSendMessage) GetStepType() string { return "StartSendMessage" }
func (startSendMessage) WaitFor(dex.Context, mailgun.SendMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSendMessage) Execute(_ dex.Context, input mailgun.SendMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[mailgun.SendMessageInput]("CallSendMessage"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the queued message ID and complete the Flow."
type completeSendMessage struct {
	dex.StepDefaultsNoWaitFor[mailgun.SendMessageResult]
}

func (completeSendMessage) GetStepType() string { return "CompleteSendMessage" }
func (completeSendMessage) Execute(ctx dex.Context, result mailgun.SendMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendFlow)(nil)
