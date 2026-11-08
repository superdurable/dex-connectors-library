// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package sourceinspection runs Segment getSource as one Dex Flow.
package sourceinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/twilio/segment/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SegmentSourceInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "segment"
	operationStepType = "CallGetSource"
)

var sourceAttribute = dex.DefineAttribute[segment.SourceProfile]("segment-source")

// Flow runs GetSource once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection segment.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection segment.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{sourceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(segment.NewGetSourceStep(segment.GetSourceStepConfig[segment.GetSourceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Segment", Explanation: "Read a source from Segment."},
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

// dex:field attribute-key:segment-source value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSource(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"segment-source": outcome}}, nil
}

// dex:field attribute-key:segment-source value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSource(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"segment-source": outcome}}, nil
}

func readSource(ctx dex.Context) (segment.SourceProfile, error) {
	outcome, err := sourceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return segment.SourceProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input segment.GetSourceInput) segment.GetSourceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSource" }
func (startRequest) WaitFor(dex.Context, segment.GetSourceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input segment.GetSourceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[segment.GetSourceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[segment.GetSourceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSource" }
func (completeResult) Execute(ctx dex.Context, result segment.GetSourceResult) (*dex.StepDecision, error) {
	if err := sourceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
