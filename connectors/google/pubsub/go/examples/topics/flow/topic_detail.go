// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topics

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const topicDetailFlowType = "GooglePubSubTopicDetail"

var topicDetailAttribute = dex.DefineAttribute[pubsub.Topic]("google-pubsub-topic")

// TopicDetailFlow reads one topic and retains the provider result.
type TopicDetailFlow struct {
	dex.FlowDefaults
	connection pubsub.Connection
}

// NewTopicDetailFlow binds the provider connection at registration.
func NewTopicDetailFlow(connection pubsub.Connection) *TopicDetailFlow {
	return &TopicDetailFlow{connection: connection}
}

func (*TopicDetailFlow) GetFlowType() string { return topicDetailFlowType }
func (*TopicDetailFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{topicDetailAttribute}}
}
func (flow *TopicDetailFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTopicDetail{}),
		dex.DefineStep(pubsub.NewGetTopicStep(pubsub.GetTopicStepConfig[pubsub.GetTopicInput]{
			StepType: "CallGetTopic", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google PubSub", Explanation: "Read the selected Pub/Sub topic."},
			Connection:  flow.connection, MapToOperationInput: mapTopicDetailInput,
			Found: sdkgo.GoTo(completeTopicDetail{}),
		})),
		dex.DefineStep(completeTopicDetail{}),
	}
}
func (flow *TopicDetailFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-pubsub-topic value-type:json editable:false description:"Selected topic"
// GetDexSummary shows the selected topic in Dex Web.
func (*TopicDetailFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	topic, err := readTopicDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-topic": topic}}, nil
}

// dex:field attribute-key:google-pubsub-topic value-type:json editable:false description:"Selected topic"
// GetDexDisplay shows the selected topic in Dex Web.
func (*TopicDetailFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	topic, err := readTopicDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-topic": topic}}, nil
}

func readTopicDetail(ctx dex.Context) (pubsub.Topic, error) {
	topic, err := topicDetailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pubsub.Topic{}, nil
	}
	return topic, err
}
func mapTopicDetailInput(input pubsub.GetTopicInput) pubsub.GetTopicInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the topic resource name to the provider Step."
type startTopicDetail struct{ dex.StepDefaults }

func (startTopicDetail) GetStepType() string { return "StartGetTopic" }
func (startTopicDetail) WaitFor(dex.Context, pubsub.GetTopicInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTopicDetail) Execute(_ dex.Context, input pubsub.GetTopicInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pubsub.GetTopicInput]("CallGetTopic"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the selected topic."
type completeTopicDetail struct {
	dex.StepDefaultsNoWaitFor[pubsub.GetTopicResult]
}

func (completeTopicDetail) GetStepType() string { return "CompleteGetTopic" }
func (completeTopicDetail) Execute(ctx dex.Context, result pubsub.GetTopicResult) (*dex.StepDecision, error) {
	if err := topicDetailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TopicDetailFlow)(nil)
