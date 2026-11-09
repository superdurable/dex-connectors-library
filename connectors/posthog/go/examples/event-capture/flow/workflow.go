// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package eventcapture defines a runnable PostHog event capture Flow.
package eventcapture

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "PostHogEventCapture"
	// ConnectionName is the static Dex Web connection used by this Flow.
	ConnectionName = "posthog"
	trackStepType  = "CapturePostHogEvent"
)

var outcomeAttribute = dex.DefineAttribute[posthog.CapturedEvent]("posthog-event-capture-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Event is the PostHog event name.
	Event string `json:"event"`
	// DistinctID identifies the associated user.
	DistinctID string `json:"distinctID"`
	// Timestamp is an RFC3339 instant; empty fixes the start time once.
	Timestamp string `json:"timestamp,omitempty"`
}

// Flow captures one PostHog event with stable deduplication fields.
type Flow struct {
	dex.FlowDefaults
	connection posthog.Connection
}

// NewFlow binds the configured PostHog connection.
func NewFlow(connection posthog.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(posthog.NewCaptureEventStep(posthog.CaptureEventStepConfig[Request]{
			StepType: trackStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "event-capture", GroupLabel: "Event capture", Explanation: "Capture one event with stable time and distinct ID for deduplication."},
			MapToOperationInput: MapToCaptureEventInput,
			Accepted:            sdkgo.GoTo(accepted{}),
		})),
		dex.DefineStep(accepted{}),
	}
}

// MapToCaptureEventInput maps a validated request to PostHog capture.
func MapToCaptureEventInput(request Request) posthog.CaptureEventInput {
	return posthog.CaptureEventInput{Event: request.Event, DistinctID: request.DistinctID, Timestamp: request.Timestamp}
}

// PrepareRequest validates event identity and fixes the timestamp before provider dispatch.
func PrepareRequest(request Request, now time.Time) (Request, error) {
	request.Event = strings.TrimSpace(request.Event)
	request.DistinctID = strings.TrimSpace(request.DistinctID)
	if request.Event == "" || request.DistinctID == "" {
		return Request{}, fmt.Errorf("event and distinctID are required")
	}
	if request.Timestamp == "" {
		request.Timestamp = now.UTC().Format(time.RFC3339Nano)
	} else if _, err := time.Parse(time.RFC3339Nano, request.Timestamp); err != nil {
		return Request{}, fmt.Errorf("timestamp must be RFC3339")
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
// dex:field attribute-key:posthog-event-capture-outcome value-type:json editable:false description:"Event capture outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-event-capture-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:posthog-event-capture-outcome value-type:json editable:false description:"Event capture outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-event-capture-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (posthog.CapturedEvent, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return posthog.CapturedEvent{}, nil
	}
	return outcome, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Validate event identity and fix its timestamp before calling PostHog."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartPostHogEventCapture" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	prepared, err := PrepareRequest(request, time.Now())
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](trackStepType), prepared), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after PostHog accepts the event for processing."
type accepted struct {
	dex.StepDefaultsNoWaitFor[posthog.CaptureEventResult]
}

func (accepted) GetStepType() string { return "PostHogEventAccepted" }
func (accepted) Execute(ctx dex.Context, result posthog.CaptureEventResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
