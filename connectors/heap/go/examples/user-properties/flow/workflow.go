// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package userproperties defines a runnable Heap property update Flow.
package userproperties

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/heap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "HeapUserProperties"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName   = "heap"
	propertyStepType = "SetHeapUserProperties"
	trackStepType    = "TrackHeapProfileEvent"
)

var outcomeAttribute = dex.DefineAttribute[heap.UserPropertiesResult]("heap-user-properties-outcome")
var inputAttribute = dex.DefineAttribute[heap.SetUserPropertiesInput]("heap-user-properties-input")

// Flow updates one user's properties through the static Heap connection.
type Flow struct {
	dex.FlowDefaults
	connection heap.Connection
}

// NewFlow binds the configured Heap connection.
func NewFlow(connection heap.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the acceptance Attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute, inputAttribute}}
}

// GetSteps wires the start, provider mutation, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(heap.NewTrackEventStep(heap.TrackEventStepConfig[heap.TrackEventInput]{
			StepType: trackStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "user-properties", GroupLabel: "User properties", Explanation: "Track a profile update event with Heap's provider deduplication key."},
			MapToOperationInput: MapToTrackEventInput, Tracked: sdkgo.GoTo(afterTrack{}),
		})),
		dex.DefineStep(afterTrack{}),
		dex.DefineStep(heap.NewSetUserPropertiesStep(heap.SetUserPropertiesStepConfig[heap.SetUserPropertiesInput]{
			StepType: propertyStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "user-properties", GroupLabel: "User properties", Explanation: "Replace selected properties for one stable Heap identity."},
			MapToOperationInput: MapToOperationInput, Accepted: sdkgo.GoTo(complete{}),
		})),
		dex.DefineStep(complete{}),
	}
}

// MapToOperationInput preserves the typed request for the connector Step.
func MapToOperationInput(input heap.SetUserPropertiesInput) heap.SetUserPropertiesInput { return input }

// MapToTrackEventInput records an event for the same identified user.
func MapToTrackEventInput(input heap.TrackEventInput) heap.TrackEventInput { return input }

// GetRPCs provides read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored provider acceptance result.
// dex:field attribute-key:heap-user-properties-outcome value-type:json editable:false description:"Property update outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"heap-user-properties-outcome": outcome}}, nil
}

// GetDexDisplay returns the stored provider acceptance result.
// dex:field attribute-key:heap-user-properties-outcome value-type:json editable:false description:"Property update outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"heap-user-properties-outcome": outcome}}, nil
}

func readOutcome(ctx dex.Context) (heap.UserPropertiesResult, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return heap.UserPropertiesResult{}, nil
	}
	return outcome, err
}

// dex:group group-id:user-properties group-label:"User properties"
// dex:explanation text:"Pass the typed identity and property values to Heap."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string { return "StartHeapUserProperties" }
func (start) WaitFor(dex.Context, heap.SetUserPropertiesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (start) Execute(ctx dex.Context, input heap.SetUserPropertiesInput) (*dex.StepDecision, error) {
	if err := inputAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[heap.TrackEventInput](trackStepType), heap.TrackEventInput{Identity: input.Identity, Event: "Profile Updated", Properties: input.Properties}), nil
}

// dex:group group-id:user-properties group-label:"User properties"
// dex:explanation text:"Set properties after Heap accepts the profile event."
type afterTrack struct {
	dex.StepDefaultsNoWaitFor[heap.TrackEventResult]
}

func (afterTrack) GetStepType() string { return "AfterHeapProfileEvent" }
func (afterTrack) Execute(ctx dex.Context, _ heap.TrackEventResult) (*dex.StepDecision, error) {
	input, err := inputAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[heap.SetUserPropertiesInput](propertyStepType), input), nil
}

// dex:group group-id:user-properties group-label:"User properties"
// dex:explanation text:"Complete after Heap acknowledges the update."
type complete struct {
	dex.StepDefaultsNoWaitFor[heap.SetUserPropertiesResult]
}

func (complete) GetStepType() string { return "HeapUserPropertiesAccepted" }
func (complete) Execute(ctx dex.Context, result heap.SetUserPropertiesResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}
