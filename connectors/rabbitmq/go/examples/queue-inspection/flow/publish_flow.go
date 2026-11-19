// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package queueinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/rabbitmq/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const publishFlowType = "RabbitMQPublishMessage"
const publishStepType = "CallPublishMessage"

var publishedAttribute = dex.DefineAttribute[rabbitmq.PublishedMessage]("rabbitmq-published-message")

// PublishFlow routes one UTF-8 message through RabbitMQ and records the routing result.
type PublishFlow struct {
	dex.FlowDefaults
	connection rabbitmq.Connection
}

// NewPublishFlow binds the existing RabbitMQ connection for message publishing.
func NewPublishFlow(connection rabbitmq.Connection) *PublishFlow {
	return &PublishFlow{connection: connection}
}

func (*PublishFlow) GetFlowType() string { return publishFlowType }

func (*PublishFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publishedAttribute}}
}

func (flow *PublishFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPublish{}),
		dex.DefineStep(rabbitmq.NewPublishMessageStep(rabbitmq.PublishMessageStepConfig[rabbitmq.PublishMessageInput]{
			StepType: publishStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "RabbitMQ", Explanation: "Publish one UTF-8 message to an exchange."},
			Connection:          flow.connection,
			MapToOperationInput: mapPublishInput,
			Published:           sdkgo.GoTo(completePublish{}),
		})),
		dex.DefineStep(completePublish{}),
	}
}

func (flow *PublishFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:rabbitmq-published-message value-type:json editable:false description:"Routing result"
// GetDexSummary returns the last routing result for Dex Web.
func (*PublishFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPublishResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rabbitmq-published-message": value}}, nil
}

// dex:field attribute-key:rabbitmq-published-message value-type:json editable:false description:"Routing result"
// GetDexDisplay returns the last routing result for Dex Web.
func (*PublishFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPublishResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"rabbitmq-published-message": value}}, nil
}

func readPublishResult(ctx dex.Context) (rabbitmq.PublishedMessage, error) {
	value, err := publishedAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return rabbitmq.PublishedMessage{}, nil
	}
	return value, err
}

func mapPublishInput(input rabbitmq.PublishMessageInput) rabbitmq.PublishMessageInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed publish input to the provider Step."
type startPublish struct{ dex.StepDefaults }

func (startPublish) GetStepType() string { return "StartPublishMessage" }
func (startPublish) WaitFor(dex.Context, rabbitmq.PublishMessageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPublish) Execute(_ dex.Context, input rabbitmq.PublishMessageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[rabbitmq.PublishMessageInput](publishStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after RabbitMQ confirms message routing."
type completePublish struct {
	dex.StepDefaultsNoWaitFor[rabbitmq.PublishMessageResult]
}

func (completePublish) GetStepType() string { return "CompletePublishMessage" }
func (completePublish) Execute(ctx dex.Context, result rabbitmq.PublishMessageResult) (*dex.StepDecision, error) {
	if err := publishedAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PublishFlow)(nil)
