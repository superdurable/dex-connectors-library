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
	// TopicAttributesFlowType is the Dex Web Start Flow identity for topic subscriptions.
	TopicAttributesFlowType     = "AmazonSNSTopicAttributes"
	topicAttributesReadStepType = "CallGetTopicAttributes"
)

var topicAttributesAttribute = dex.DefineAttribute[sns.TopicAttributes]("sns-topic-attributes")

// TopicAttributesFlow lists one page of attributes for a topic.
type TopicAttributesFlow struct {
	dex.FlowDefaults
	connection sns.Connection
}

// NewTopicAttributesFlow binds the SNS connection for subscription listing.
func NewTopicAttributesFlow(connection sns.Connection) *TopicAttributesFlow {
	return &TopicAttributesFlow{connection: connection}
}

// GetFlowType returns the stable subscription listing Flow identity.
func (*TopicAttributesFlow) GetFlowType() string { return TopicAttributesFlowType }

// GetPersistenceSchema registers the topic attributes.
func (*TopicAttributesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{topicAttributesAttribute}}
}

// GetRPCs exposes the topic attributes in Dex Web.
func (flow *TopicAttributesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the last topic attributes.
//
// dex:field attribute-key:sns-topic-attributes value-type:json editable:false description:"SNS topic attributes"
func (*TopicAttributesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTopicAttributes(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-topic-attributes": page}}, nil
}

// GetDexDisplay returns the last topic attributes.
//
// dex:field attribute-key:sns-topic-attributes value-type:json editable:false description:"SNS topic attributes"
func (*TopicAttributesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTopicAttributes(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-topic-attributes": page}}, nil
}

func readTopicAttributes(ctx dex.Context) (sns.TopicAttributes, error) {
	page, err := topicAttributesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sns.TopicAttributes{}, nil
	}
	return page, err
}

// GetSteps registers start, provider list, and completion Steps.
func (flow *TopicAttributesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTopicAttributes{}),
		dex.DefineStep(sns.NewGetTopicAttributesStep(sns.GetTopicAttributesStepConfig[sns.TopicARNInput]{
			StepType: topicAttributesReadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SNS", Explanation: "List one page of attributes of the selected SNS topic."},
			Connection:  flow.connection, MapToOperationInput: mapToTopicAttributesInput,
			Found: sdkgo.GoTo(completeTopicAttributes{}),
		})),
		dex.DefineStep(completeTopicAttributes{}),
	}
}

func mapToTopicAttributesInput(input sns.TopicARNInput) sns.TopicARNInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the topic ARN and optional page token to SNS."
type startTopicAttributes struct{ dex.StepDefaults }

func (startTopicAttributes) GetStepType() string { return "StartGetTopicAttributes" }
func (startTopicAttributes) WaitFor(dex.Context, sns.TopicARNInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTopicAttributes) Execute(_ dex.Context, input sns.TopicARNInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sns.TopicARNInput](topicAttributesReadStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one SNS topic's attributes."
type completeTopicAttributes struct {
	dex.StepDefaultsNoWaitFor[sns.GetTopicAttributesResult]
}

func (completeTopicAttributes) GetStepType() string { return "CompleteGetTopicAttributes" }
func (completeTopicAttributes) Execute(ctx dex.Context, result sns.GetTopicAttributesResult) (*dex.StepDecision, error) {
	if err := topicAttributesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TopicAttributesFlow)(nil)
