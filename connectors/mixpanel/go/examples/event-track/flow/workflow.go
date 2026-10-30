// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package eventtrack defines a runnable Mixpanel event import Flow.
package eventtrack

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/mixpanel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "MixpanelEventTrack"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName = "mixpanel"
	trackStepType  = "ImportMixpanelEvent"
)

var outcomeAttribute = dex.DefineAttribute[mixpanel.EventImportResult]("mixpanel-event-track-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Event is the Mixpanel event name.
	Event string `json:"event"`
	// DistinctID identifies the associated user.
	DistinctID string `json:"distinctID"`
	// Time is a Unix timestamp in seconds; zero fixes the start time once.
	Time int64 `json:"time,omitempty"`
}

// Flow imports one Mixpanel event with stable deduplication fields.
type Flow struct {
	dex.FlowDefaults
	connection mixpanel.Connection
}

// NewFlow binds the configured Mixpanel connection.
func NewFlow(connection mixpanel.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(mixpanel.NewTrackEventStep(mixpanel.TrackEventStepConfig[Request]{
			StepType: trackStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "event-track", GroupLabel: "Event track", Explanation: "Import one event with stable time and distinct ID for deduplication."},
			MapToOperationInput: MapToTrackEventInput,
			Imported:            sdkgo.GoTo(imported{}),
		})),
		dex.DefineStep(imported{}),
	}
}

// MapToTrackEventInput maps a validated request to Mixpanel's import operation.
func MapToTrackEventInput(request Request) mixpanel.TrackEventInput {
	return mixpanel.TrackEventInput{Event: request.Event, DistinctID: request.DistinctID, Time: request.Time}
}

// PrepareRequest validates event identity and fixes the timestamp before provider dispatch.
func PrepareRequest(request Request, now time.Time) (Request, error) {
	request.Event = strings.TrimSpace(request.Event)
	request.DistinctID = strings.TrimSpace(request.DistinctID)
	if request.Event == "" || request.DistinctID == "" {
		return Request{}, fmt.Errorf("event and distinctID are required")
	}
	if request.Time == 0 {
		request.Time = now.Unix()
	}
	return request, nil
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:mixpanel-event-track-outcome value-type:json editable:false description:"Event import outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mixpanel-event-track-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:mixpanel-event-track-outcome value-type:json editable:false description:"Event import outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"mixpanel-event-track-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (mixpanel.EventImportResult, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return mixpanel.EventImportResult{}, nil
	}
	return outcome, err
}

// dex:group group-id:event-track group-label:"Event track"
// dex:explanation text:"Validate event identity and fix its timestamp before calling Mixpanel."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartMixpanelEventTrack" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	prepared, err := PrepareRequest(request, time.Now())
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](trackStepType), prepared), nil
}

// dex:group group-id:event-track group-label:"Event track"
// dex:explanation text:"Complete after Mixpanel confirms event ingestion."
type imported struct {
	dex.StepDefaultsNoWaitFor[mixpanel.TrackEventResult]
}

func (imported) GetStepType() string { return "MixpanelEventImported" }
func (imported) Execute(ctx dex.Context, result mixpanel.TrackEventResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
