// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package subscribercount runs WxPusher getSubscriberCount as one Dex Flow.
package subscribercount

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/wxpusher/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WxPusherSubscriberCount"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "wxpusher"
	operationStepType = "CallGetSubscriberCount"
)

var subscriberCountOutcomeAttribute = dex.DefineAttribute[wxpusher.SubscriberCountResult]("wxpusher-subscriber-count-outcome")

// Flow runs GetSubscriberCount once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection wxpusher.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection wxpusher.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{subscriberCountOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wxpusher.NewGetSubscriberCountStep(wxpusher.GetSubscriberCountStepConfig[wxpusher.GetSubscriberCountInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WxPusher", Explanation: "Count subscription records for the authorized application."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Counted:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wxpusher-subscriber-count-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscriberCountOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wxpusher-subscriber-count-outcome": outcome}}, nil
}

// dex:field attribute-key:wxpusher-subscriber-count-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSubscriberCountOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wxpusher-subscriber-count-outcome": outcome}}, nil
}

func readSubscriberCountOutcome(ctx dex.Context) (wxpusher.SubscriberCountResult, error) {
	outcome, err := subscriberCountOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wxpusher.SubscriberCountResult{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input wxpusher.GetSubscriberCountInput) wxpusher.GetSubscriberCountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSubscriberCount" }
func (startRequest) WaitFor(dex.Context, wxpusher.GetSubscriberCountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wxpusher.GetSubscriberCountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wxpusher.GetSubscriberCountInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wxpusher.GetSubscriberCountResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSubscriberCount" }
func (completeResult) Execute(ctx dex.Context, result wxpusher.GetSubscriberCountResult) (*dex.StepDecision, error) {
	if err := subscriberCountOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
