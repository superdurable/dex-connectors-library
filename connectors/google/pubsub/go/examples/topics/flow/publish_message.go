// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topics

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const publishedMessageFlowType = "GooglePubSubPublish"

var publishedMessageAttribute = dex.DefineAttribute[pubsub.PublishedMessage]("google-pubsub-published-message")

// PublishFlow publishes one text message and retains its provider ID.
type PublishFlow struct {
	dex.FlowDefaults
	connection pubsub.Connection
}

// NewPublishFlow binds the provider connection at registration.
func NewPublishFlow(connection pubsub.Connection) *PublishFlow {
	return &PublishFlow{connection: connection}
}

func (*PublishFlow) GetFlowType() string { return publishedMessageFlowType }
func (*PublishFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publishedMessageAttribute}}
}
func (flow *PublishFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPublish{}),
		dex.DefineStep(pubsub.NewPublishMessageStep(pubsub.PublishMessageStepConfig[pubsub.PublishMessageInput]{
			StepType: "CallPublishMessage", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google PubSub", Explanation: "Publish a text message to the selected topic."},
			Connection:  flow.connection, MapToOperationInput: mapPublishInput,
			Published: sdkgo.GoTo(completePublish{}),
		})),
		dex.DefineStep(completePublish{}),
	}
}
func (flow *PublishFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-pubsub-published-message value-type:json editable:false description:"Published message ID"
// GetDexSummary shows the published message ID in Dex Web.
func (*PublishFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readPublish(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-published-message": message}}, nil
}

// dex:field attribute-key:google-pubsub-published-message value-type:json editable:false description:"Published message ID"
// GetDexDisplay shows the published message ID in Dex Web.
func (*PublishFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	message, err := readPublish(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-published-message": message}}, nil
}

func readPublish(ctx dex.Context) (pubsub.PublishedMessage, error) {
	message, err := publishedMessageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pubsub.PublishedMessage{}, nil
	}
	return message, err
}
func mapPublishInput(input pubsub.PublishMessageInput) pubsub.PublishMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed text message to the provider Step."
type startPublish struct{ dex.StepDefaults }

func (startPublish) GetStepType() string { return "StartPublishMessage" }
func (startPublish) WaitFor(dex.Context, pubsub.PublishMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPublish) Execute(_ dex.Context, input pubsub.PublishMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pubsub.PublishMessageInput]("CallPublishMessage"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider message ID."
type completePublish struct {
	dex.StepDefaultsNoWaitFor[pubsub.PublishMessageResult]
}

func (completePublish) GetStepType() string { return "CompletePublishMessage" }
func (completePublish) Execute(ctx dex.Context, result pubsub.PublishMessageResult) (*dex.StepDecision, error) {
	if err := publishedMessageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PublishFlow)(nil)
