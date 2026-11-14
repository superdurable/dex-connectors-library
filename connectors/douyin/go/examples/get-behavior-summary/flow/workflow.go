// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package behaviorsummary runs Douyin getBehaviorSummary as one Dex Flow.
package behaviorsummary

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/douyin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DouyinBehaviorSummary"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "douyin"
	operationStepType = "CallGetBehaviorSummary"
)

var behaviorSummaryOutcomeAttribute = dex.DefineAttribute[douyin.GetBehaviorSummaryOutput]("douyin-get-behavior-summary-outcome")

// Flow runs GetBehaviorSummary once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection douyin.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection douyin.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{behaviorSummaryOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(douyin.NewGetBehaviorSummaryStep(douyin.GetBehaviorSummaryStepConfig[douyin.GetBehaviorSummaryInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Douyin", Explanation: "Read Douyin mini app behavior summary for the supplied reporting date."},
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

// dex:field attribute-key:douyin-get-behavior-summary-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetBehaviorSummaryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"douyin-get-behavior-summary-outcome": outcome}}, nil
}

// dex:field attribute-key:douyin-get-behavior-summary-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetBehaviorSummaryOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"douyin-get-behavior-summary-outcome": outcome}}, nil
}

func readGetBehaviorSummaryOutcome(ctx dex.Context) (douyin.GetBehaviorSummaryOutput, error) {
	outcome, err := behaviorSummaryOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return douyin.GetBehaviorSummaryOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input douyin.GetBehaviorSummaryInput) douyin.GetBehaviorSummaryInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetBehaviorSummary" }
func (startRequest) WaitFor(dex.Context, douyin.GetBehaviorSummaryInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input douyin.GetBehaviorSummaryInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[douyin.GetBehaviorSummaryInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[douyin.GetBehaviorSummaryResult]
}

func (completeResult) GetStepType() string { return "CompleteGetBehaviorSummary" }
func (completeResult) Execute(ctx dex.Context, result douyin.GetBehaviorSummaryResult) (*dex.StepDecision, error) {
	if err := behaviorSummaryOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
