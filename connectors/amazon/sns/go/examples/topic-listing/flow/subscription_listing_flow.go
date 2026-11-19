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
	// SubscriptionListingFlowType is the Dex Web Start Flow identity for topic subscriptions.
	SubscriptionListingFlowType = "AmazonSNSSubscriptionListing"
	subscriptionListStepType    = "CallListSubscriptionsByTopic"
)

var subscriptionPageAttribute = dex.DefineAttribute[sns.SubscriptionPage]("sns-subscription-listing")

// SubscriptionListingFlow lists one page of subscriptions for a topic.
type SubscriptionListingFlow struct {
	dex.FlowDefaults
	connection sns.Connection
}

// NewSubscriptionListingFlow binds the SNS connection for subscription listing.
func NewSubscriptionListingFlow(connection sns.Connection) *SubscriptionListingFlow {
	return &SubscriptionListingFlow{connection: connection}
}

// GetFlowType returns the stable subscription listing Flow identity.
func (*SubscriptionListingFlow) GetFlowType() string { return SubscriptionListingFlowType }

// GetPersistenceSchema registers the subscription page.
func (*SubscriptionListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriptionPageAttribute}}
}

// GetRPCs exposes the subscription page in Dex Web.
func (flow *SubscriptionListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the last subscription page.
//
// dex:field attribute-key:sns-subscription-listing value-type:json editable:false description:"SNS subscription page"
func (*SubscriptionListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriptionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-subscription-listing": page}}, nil
}

// GetDexDisplay returns the last subscription page.
//
// dex:field attribute-key:sns-subscription-listing value-type:json editable:false description:"SNS subscription page"
func (*SubscriptionListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriptionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sns-subscription-listing": page}}, nil
}

func readSubscriptionPage(ctx dex.Context) (sns.SubscriptionPage, error) {
	page, err := subscriptionPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sns.SubscriptionPage{}, nil
	}
	return page, err
}

// GetSteps registers start, provider list, and completion Steps.
func (flow *SubscriptionListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriptionListing{}),
		dex.DefineStep(sns.NewListSubscriptionsByTopicStep(sns.ListSubscriptionsByTopicStepConfig[sns.ListSubscriptionsByTopicInput]{
			StepType: subscriptionListStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amazon SNS", Explanation: "List one page of subscriptions to the selected SNS topic."},
			Connection:  flow.connection, MapToOperationInput: mapToSubscriptionListInput,
			Page: sdkgo.GoTo(completeSubscriptionListing{}),
		})),
		dex.DefineStep(completeSubscriptionListing{}),
	}
}

func mapToSubscriptionListInput(input sns.ListSubscriptionsByTopicInput) sns.ListSubscriptionsByTopicInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the topic ARN and optional page token to SNS."
type startSubscriptionListing struct{ dex.StepDefaults }

func (startSubscriptionListing) GetStepType() string { return "StartListSubscriptionsByTopic" }
func (startSubscriptionListing) WaitFor(dex.Context, sns.ListSubscriptionsByTopicInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriptionListing) Execute(_ dex.Context, input sns.ListSubscriptionsByTopicInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sns.ListSubscriptionsByTopicInput](subscriptionListStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one SNS subscription page."
type completeSubscriptionListing struct {
	dex.StepDefaultsNoWaitFor[sns.ListSubscriptionsByTopicResult]
}

func (completeSubscriptionListing) GetStepType() string { return "CompleteListSubscriptionsByTopic" }
func (completeSubscriptionListing) Execute(ctx dex.Context, result sns.ListSubscriptionsByTopicResult) (*dex.StepDecision, error) {
	if err := subscriptionPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriptionListingFlow)(nil)
