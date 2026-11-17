// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueconsumer

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sqs/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// SendFlowType is the stable Dex Web Start Flow identity for sending one message.
	SendFlowType        = "AmazonSQSSendMessage"
	sendMessageStepType = "CallSendMessage"
)

var sentMessageAttribute = dex.DefineAttribute[sqs.SentMessage]("amazon-sqs-sent-message")

// SendFlow sends one message through the consumer example's SQS connection.
type SendFlow struct {
	dex.FlowDefaults
	connection sqs.Connection
}

// NewSendFlow binds the provider connection at registration time.
func NewSendFlow(connection sqs.Connection) *SendFlow { return &SendFlow{connection: connection} }

// GetFlowType returns the stable send Flow identity.
func (*SendFlow) GetFlowType() string { return SendFlowType }

// GetPersistenceSchema registers the sent message identity.
func (*SendFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sentMessageAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the message ID.
func (flow *SendFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:amazon-sqs-sent-message value-type:json editable:false description:"Accepted SQS message"
func (*SendFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-sqs-sent-message": message}}, nil
}

// GetDexDisplay returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:amazon-sqs-sent-message value-type:json editable:false description:"Accepted SQS message"
func (*SendFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readSentMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amazon-sqs-sent-message": message}}, nil
}

func readSentMessage(ctx dex.Context) (sqs.SentMessage, error) {
	message, err := sentMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sqs.SentMessage{}, nil
	}
	return message, err
}

// GetSteps registers the start, provider send, and completion Steps.
func (flow *SendFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMessageSend{}),
		dex.DefineStep(sqs.NewSendMessageStep(sqs.SendMessageStepConfig[sqs.SendMessageInput]{
			StepType: sendMessageStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SQS", Explanation: "Send one message to a standard or FIFO SQS queue."},
			Connection:  flow.connection, MapToOperationInput: mapToSendMessageInput,
			Sent: sdkgo.GoTo(completeMessageSend{}),
		})),
		dex.DefineStep(completeMessageSend{}),
	}
}

func mapToSendMessageInput(input sqs.SendMessageInput) sqs.SendMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the queue and message to the SQS provider Step."
type startMessageSend struct{ dex.StepDefaults }

func (startMessageSend) GetStepType() string { return "StartSendMessage" }
func (startMessageSend) WaitFor(dex.Context, sqs.SendMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMessageSend) Execute(_ dex.Context, input sqs.SendMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sqs.SendMessageInput](sendMessageStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SQS's accepted message identity."
type completeMessageSend struct {
	dex.StepDefaultsNoWaitFor[sqs.SendMessageResult]
}

func (completeMessageSend) GetStepType() string { return "CompleteSendMessage" }
func (completeMessageSend) Execute(ctx dex.Context, result sqs.SendMessageResult) (*dex.StepDecision, error) {
	if err := sentMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SendFlow)(nil)
