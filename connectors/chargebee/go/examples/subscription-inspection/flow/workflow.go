// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package subscriptioninspection runs Chargebee getSubscription as one Dex Flow.
package subscriptioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/chargebee/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ChargebeeSubscriptionInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "chargebee"
	operationStepType = "CallGetSubscription"
)

var subscriptionAttribute = dex.DefineAttribute[chargebee.SubscriptionStatus]("chargebee-subscription")

// Flow runs GetSubscription once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection chargebee.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection chargebee.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriptionAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(chargebee.NewGetSubscriptionStep(chargebee.GetSubscriptionStepConfig[chargebee.GetSubscriptionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Chargebee", Explanation: "Read a subscription from Chargebee."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:chargebee-subscription value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscription(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chargebee-subscription": outcome}}, nil
}

// dex:field attribute-key:chargebee-subscription value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscription(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chargebee-subscription": outcome}}, nil
}

func readSubscription(ctx dex.Context) (chargebee.SubscriptionStatus, error) {
	outcome, err := subscriptionAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return chargebee.SubscriptionStatus{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input chargebee.GetSubscriptionInput) chargebee.GetSubscriptionInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSubscription" }
func (startRequest) WaitFor(dex.Context, chargebee.GetSubscriptionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input chargebee.GetSubscriptionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[chargebee.GetSubscriptionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[chargebee.GetSubscriptionResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSubscription" }
func (completeResult) Execute(ctx dex.Context, result chargebee.GetSubscriptionResult) (*dex.StepDecision, error) {
	if err := subscriptionAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
