// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package usersearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// TrackFlowType is the stable Dex Web Start Flow identity for one event.
	TrackFlowType      = "AmplitudeTrackEvent"
	trackEventStepType = "CallTrackEvent"
)

var trackedEventAttribute = dex.DefineAttribute[amplitude.TrackedEvent]("amplitude-tracked-event")

// TrackFlow uploads one event through the example's Amplitude connection.
type TrackFlow struct {
	dex.FlowDefaults
	connection amplitude.Connection
}

// NewTrackFlow binds the provider connection at registration time.
func NewTrackFlow(connection amplitude.Connection) *TrackFlow {
	return &TrackFlow{connection: connection}
}

// GetFlowType returns the stable event Flow identity.
func (*TrackFlow) GetFlowType() string { return TrackFlowType }

// GetPersistenceSchema registers the accepted event identity.
func (*TrackFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{trackedEventAttribute}}
}

// GetRPCs exposes read-only Dex Web views of the accepted event.
func (flow *TrackFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the accepted event identity when this Flow has one.
//
// dex:field attribute-key:amplitude-tracked-event value-type:json editable:false description:"Accepted Amplitude event"
func (*TrackFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readTrackedEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-tracked-event": event}}, nil
}

// GetDexDisplay returns the accepted event identity when this Flow has one.
//
// dex:field attribute-key:amplitude-tracked-event value-type:json editable:false description:"Accepted Amplitude event"
func (*TrackFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	event, err := readTrackedEvent(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-tracked-event": event}}, nil
}

func readTrackedEvent(ctx dex.Context) (amplitude.TrackedEvent, error) {
	event, err := trackedEventAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return amplitude.TrackedEvent{}, nil
	}
	return event, err
}

// GetSteps registers the start, provider upload, and completion Steps.
func (flow *TrackFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startEventTrack{}),
		dex.DefineStep(amplitude.NewTrackEventStep(amplitude.TrackEventStepConfig[amplitude.TrackEventInput]{
			StepType: trackEventStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amplitude", Explanation: "Upload one project analytics event with a stable insert ID."},
			Connection:  flow.connection, MapToOperationInput: mapToTrackInput,
			Tracked: sdkgo.GoTo(completeEventTrack{}),
		})),
		dex.DefineStep(completeEventTrack{}),
	}
}

func mapToTrackInput(input amplitude.TrackEventInput) amplitude.TrackEventInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the event to the Amplitude provider Step."
type startEventTrack struct{ dex.StepDefaults }

func (startEventTrack) GetStepType() string { return "StartTrackEvent" }
func (startEventTrack) WaitFor(dex.Context, amplitude.TrackEventInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startEventTrack) Execute(_ dex.Context, input amplitude.TrackEventInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[amplitude.TrackEventInput](trackEventStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the deduplicated event identity."
type completeEventTrack struct {
	dex.StepDefaultsNoWaitFor[amplitude.TrackEventResult]
}

func (completeEventTrack) GetStepType() string { return "CompleteTrackEvent" }
func (completeEventTrack) Execute(ctx dex.Context, result amplitude.TrackEventResult) (*dex.StepDecision, error) {
	if err := trackedEventAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TrackFlow)(nil)
