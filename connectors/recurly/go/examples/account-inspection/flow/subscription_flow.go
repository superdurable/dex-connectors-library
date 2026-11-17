// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountinspection runs Recurly getSubscription as one Dex Flow.
package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// SubscriptionFlowType is the stable example Flow identity.
	SubscriptionFlowType = "RecurlySubscriptionInspection"
	subscriptionStepType = "CallGetSubscription"
)

var subscriptionAttribute = dex.DefineAttribute[recurly.SubscriptionSummary]("recurly-subscription")

// SubscriptionFlow runs GetSubscription once and completes with its typed output.
type SubscriptionFlow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewSubscriptionFlow binds the provider connection at registration time.
func NewSubscriptionFlow(connection recurly.Connection) *SubscriptionFlow {
	return &SubscriptionFlow{connection: connection}
}

func (*SubscriptionFlow) GetFlowType() string { return SubscriptionFlowType }

func (*SubscriptionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriptionAttribute}}
}

func (flow *SubscriptionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSubscriptionRequest{}),
		dex.DefineStep(recurly.NewGetSubscriptionStep(recurly.GetSubscriptionStepConfig[recurly.GetSubscriptionInput]{
			StepType: subscriptionStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Read a subscription from Recurly."},
			Connection:          flow.connection,
			MapToOperationInput: mapSubscriptionInput,
			Found:               sdkgo.GoTo(completeSubscriptionResult{}),
		})),
		dex.DefineStep(completeSubscriptionResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *SubscriptionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:recurly-subscription value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*SubscriptionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscription(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-subscription": outcome}}, nil
}

// dex:field attribute-key:recurly-subscription value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*SubscriptionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscription(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-subscription": outcome}}, nil
}

func readSubscription(ctx dex.Context) (recurly.SubscriptionSummary, error) {
	outcome, err := subscriptionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.SubscriptionSummary{}, nil
	}
	return outcome, err
}

// mapSubscriptionInput preserves the validated typed start input for the provider call.
func mapSubscriptionInput(input recurly.GetSubscriptionInput) recurly.GetSubscriptionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startSubscriptionRequest struct{ dex.StepDefaults }

func (startSubscriptionRequest) GetStepType() string { return "StartGetSubscription" }
func (startSubscriptionRequest) WaitFor(dex.Context, recurly.GetSubscriptionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSubscriptionRequest) Execute(_ dex.Context, input recurly.GetSubscriptionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.GetSubscriptionInput](subscriptionStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeSubscriptionResult struct {
	dex.StepDefaultsNoWaitFor[recurly.GetSubscriptionResult]
}

func (completeSubscriptionResult) GetStepType() string { return "CompleteGetSubscription" }
func (completeSubscriptionResult) Execute(ctx dex.Context, result recurly.GetSubscriptionResult) (*dex.StepDecision, error) {
	if err := subscriptionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SubscriptionFlow)(nil)
