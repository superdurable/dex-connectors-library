// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package topics

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/pubsub/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const subscriptionDetailFlowType = "GooglePubSubSubscriptionDetail"

var subscriptionDetailAttribute = dex.DefineAttribute[pubsub.Subscription]("google-pubsub-subscription")

// SubscriptionDetailFlow reads one subscription and retains the provider result.
type SubscriptionDetailFlow struct {
	dex.FlowDefaults
	connection pubsub.Connection
}

// NewSubscriptionDetailFlow binds the provider connection at registration.
func NewSubscriptionDetailFlow(connection pubsub.Connection) *SubscriptionDetailFlow {
	return &SubscriptionDetailFlow{connection: connection}
}

func (*SubscriptionDetailFlow) GetFlowType() string { return subscriptionDetailFlowType }
func (*SubscriptionDetailFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriptionDetailAttribute}}
}
func (flow *SubscriptionDetailFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriptionDetail{}),
		dex.DefineStep(pubsub.NewGetSubscriptionStep(pubsub.GetSubscriptionStepConfig[pubsub.GetSubscriptionInput]{
			StepType: "CallGetSubscription", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google PubSub", Explanation: "Read the selected Pub/Sub subscription."},
			Connection:  flow.connection, MapToOperationInput: mapSubscriptionDetailInput,
			Found: sdkgo.GoTo(completeSubscriptionDetail{}),
		})),
		dex.DefineStep(completeSubscriptionDetail{}),
	}
}
func (flow *SubscriptionDetailFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-pubsub-subscription value-type:json editable:false description:"Selected subscription"
// GetDexSummary shows the selected subscription in Dex Web.
func (*SubscriptionDetailFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	subscription, err := readSubscriptionDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-subscription": subscription}}, nil
}

// dex:field attribute-key:google-pubsub-subscription value-type:json editable:false description:"Selected subscription"
// GetDexDisplay shows the selected subscription in Dex Web.
func (*SubscriptionDetailFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	subscription, err := readSubscriptionDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-pubsub-subscription": subscription}}, nil
}

func readSubscriptionDetail(ctx dex.Context) (pubsub.Subscription, error) {
	subscription, err := subscriptionDetailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return pubsub.Subscription{}, nil
	}
	return subscription, err
}
func mapSubscriptionDetailInput(input pubsub.GetSubscriptionInput) pubsub.GetSubscriptionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the subscription resource name to the provider Step."
type startSubscriptionDetail struct{ dex.StepDefaults }

func (startSubscriptionDetail) GetStepType() string { return "StartGetSubscription" }
func (startSubscriptionDetail) WaitFor(dex.Context, pubsub.GetSubscriptionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriptionDetail) Execute(_ dex.Context, input pubsub.GetSubscriptionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[pubsub.GetSubscriptionInput]("CallGetSubscription"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the selected subscription."
type completeSubscriptionDetail struct {
	dex.StepDefaultsNoWaitFor[pubsub.GetSubscriptionResult]
}

func (completeSubscriptionDetail) GetStepType() string { return "CompleteGetSubscription" }
func (completeSubscriptionDetail) Execute(ctx dex.Context, result pubsub.GetSubscriptionResult) (*dex.StepDecision, error) {
	if err := subscriptionDetailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriptionDetailFlow)(nil)
