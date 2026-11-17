// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topiclisting

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// PublishFlowType is the stable Dex Web Start Flow identity for a topic message.
	PublishFlowType = "AmazonSNSPublishMessage"
	publishStepType = "CallPublishMessage"
)

var publishedMessageAttribute = dex.DefineAttribute[sns.PublishedMessage]("sns-published-message")

// PublishFlow sends one message through the same SNS connection as the listing Flow.
type PublishFlow struct {
	dex.FlowDefaults
	connection sns.Connection
}

// NewPublishFlow binds the provider connection for one message Flow.
func NewPublishFlow(connection sns.Connection) *PublishFlow {
	return &PublishFlow{connection: connection}
}

// GetFlowType returns the stable publish Flow identity.
func (*PublishFlow) GetFlowType() string { return PublishFlowType }

// GetPersistenceSchema registers the accepted message identity.
func (*PublishFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publishedMessageAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the message ID.
func (flow *PublishFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:sns-published-message value-type:json editable:false description:"Accepted SNS message ID"
func (*PublishFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readPublishedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-published-message": message}}, nil
}

// GetDexDisplay returns the accepted message ID when this Flow has one.
//
// dex:field attribute-key:sns-published-message value-type:json editable:false description:"Accepted SNS message ID"
func (*PublishFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readPublishedMessage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-published-message": message}}, nil
}

func readPublishedMessage(ctx dex.Context) (sns.PublishedMessage, error) {
	message, err := publishedMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sns.PublishedMessage{}, nil
	}
	return message, err
}

// GetSteps registers the start, provider publish, and completion Steps.
func (flow *PublishFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMessagePublish{}),
		dex.DefineStep(sns.NewPublishMessageStep(sns.PublishMessageStepConfig[sns.PublishMessageInput]{
			StepType: publishStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SNS", Explanation: "Publish one UTF-8 message to a regional SNS topic."},
			Connection:  flow.connection, MapToOperationInput: mapToPublishInput,
			Published: sdkgo.GoTo(completeMessagePublish{}),
		})),
		dex.DefineStep(completeMessagePublish{}),
	}
}

func mapToPublishInput(input sns.PublishMessageInput) sns.PublishMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the topic and message to the SNS provider Step."
type startMessagePublish struct{ dex.StepDefaults }

func (startMessagePublish) GetStepType() string { return "StartPublishMessage" }
func (startMessagePublish) WaitFor(dex.Context, sns.PublishMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMessagePublish) Execute(_ dex.Context, input sns.PublishMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sns.PublishMessageInput](publishStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with SNS's accepted message ID."
type completeMessagePublish struct {
	dex.StepDefaultsNoWaitFor[sns.PublishMessageResult]
}

func (completeMessagePublish) GetStepType() string { return "CompletePublishMessage" }
func (completeMessagePublish) Execute(ctx dex.Context, result sns.PublishMessageResult) (*dex.StepDecision, error) {
	if err := publishedMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PublishFlow)(nil)
