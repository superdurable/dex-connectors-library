// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package topiclisting runs Amazon SNS listTopics as one Dex Flow.
package topiclisting

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amazon/sns/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AmazonSNSTopicListing"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "amazon-sns"
	operationStepType = "CallListTopics"
)

var topicPageAttribute = dex.DefineAttribute[sns.TopicPage]("sns-topic-listing")

// Flow runs ListTopics once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sns.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sns.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{topicPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sns.NewListTopicsStep(sns.ListTopicsStepConfig[sns.ListTopicsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SNS", Explanation: "Read one SNS page of regional topic ARNs."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Page:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:sns-topic-listing value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTopicPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-topic-listing": outcome}}, nil
}

// dex:field attribute-key:sns-topic-listing value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTopicPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-topic-listing": outcome}}, nil
}

func readTopicPage(ctx dex.Context) (sns.TopicPage, error) {
	outcome, err := topicPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sns.TopicPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sns.ListTopicsInput) sns.ListTopicsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListTopics" }
func (startRequest) WaitFor(dex.Context, sns.ListTopicsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sns.ListTopicsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sns.ListTopicsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sns.ListTopicsResult]
}

func (completeResult) GetStepType() string { return "CompleteListTopics" }
func (completeResult) Execute(ctx dex.Context, result sns.ListTopicsResult) (*dex.StepDecision, error) {
	if err := topicPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
