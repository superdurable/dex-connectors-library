// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package segmenttext runs Gugudata segmentText as one Dex Flow.
package segmenttext

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/gugudata/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GugudataSegmentText"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "gugudata"
	operationStepType = "CallSegmentText"
)

var segmentTextOutcomeAttribute = dex.DefineAttribute[gugudata.SegmentTextOutput]("gugudata-segment-text-outcome")

// Flow runs SegmentText once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection gugudata.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection gugudata.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{segmentTextOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(gugudata.NewSegmentTextStep(gugudata.SegmentTextStepConfig[gugudata.SegmentTextInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Gugudata", Explanation: "Segment the requested short text."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Segmented:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:gugudata-segment-text-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentTextOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gugudata-segment-text-outcome": outcome}}, nil
}

// dex:field attribute-key:gugudata-segment-text-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSegmentTextOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gugudata-segment-text-outcome": outcome}}, nil
}

func readSegmentTextOutcome(ctx dex.Context) (gugudata.SegmentTextOutput, error) {
	outcome, err := segmentTextOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return gugudata.SegmentTextOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input gugudata.SegmentTextInput) gugudata.SegmentTextInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSegmentText" }
func (startRequest) WaitFor(dex.Context, gugudata.SegmentTextInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input gugudata.SegmentTextInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[gugudata.SegmentTextInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[gugudata.SegmentTextResult]
}

func (completeResult) GetStepType() string { return "CompleteSegmentText" }
func (completeResult) Execute(ctx dex.Context, result gugudata.SegmentTextResult) (*dex.StepDecision, error) {
	if err := segmentTextOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
