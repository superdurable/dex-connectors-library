// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// SubscriptionFlowType is the stable Paddle subscription listing Flow identity.
const SubscriptionFlowType = "PaddleSubscriptionListing"

const subscriptionStepType = "CallListSubscriptions"

var subscriptionPageAttribute = dex.DefineAttribute[paddle.SubscriptionPage]("paddle-subscription-page")

// SubscriptionFlow lists one page of Paddle subscriptions.
type SubscriptionFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewSubscriptionFlow binds the Paddle connection for listing subscriptions.
func NewSubscriptionFlow(connection paddle.Connection) *SubscriptionFlow {
	return &SubscriptionFlow{connection: connection}
}

func (*SubscriptionFlow) GetFlowType() string { return SubscriptionFlowType }

func (*SubscriptionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriptionPageAttribute}}
}

func (flow *SubscriptionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriptionList{}),
		dex.DefineStep(paddle.NewListSubscriptionsStep(paddle.ListSubscriptionsStepConfig[paddle.ListSubscriptionsInput]{
			StepType: subscriptionStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle subscriptions."},
			Connection:  flow.connection, MapToOperationInput: mapSubscriptionListInput,
			Listed: sdkgo.GoTo(completeSubscriptionList{}),
		})),
		dex.DefineStep(completeSubscriptionList{}),
	}
}

func (flow *SubscriptionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-subscription-page value-type:json editable:false description:"Subscription page"
func (*SubscriptionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriptionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-subscription-page": page}}, nil
}

// dex:field attribute-key:paddle-subscription-page value-type:json editable:false description:"Subscription page"
func (*SubscriptionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readSubscriptionPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-subscription-page": page}}, nil
}

func readSubscriptionPage(ctx dex.Context) (paddle.SubscriptionPage, error) {
	page, err := subscriptionPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return paddle.SubscriptionPage{}, nil
	}
	return page, err
}

func mapSubscriptionListInput(input paddle.ListSubscriptionsInput) paddle.ListSubscriptionsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested subscription page and cursor to Paddle."
type startSubscriptionList struct{ dex.StepDefaults }

func (startSubscriptionList) GetStepType() string { return "StartListSubscriptions" }
func (startSubscriptionList) WaitFor(dex.Context, paddle.ListSubscriptionsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriptionList) Execute(_ dex.Context, input paddle.ListSubscriptionsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListSubscriptionsInput](subscriptionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned subscription page."
type completeSubscriptionList struct {
	dex.StepDefaultsNoWaitFor[paddle.ListSubscriptionsResult]
}

func (completeSubscriptionList) GetStepType() string { return "CompleteListSubscriptions" }
func (completeSubscriptionList) Execute(ctx dex.Context, result paddle.ListSubscriptionsResult) (*dex.StepDecision, error) {
	if err := subscriptionPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriptionFlow)(nil)
