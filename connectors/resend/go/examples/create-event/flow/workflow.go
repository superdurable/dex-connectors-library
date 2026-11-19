// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createevent demonstrates Resend draft event creation.
package createevent

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateEvent"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new event settings.
type Request resend.CreateEventInput

var outcomeAttribute = dex.DefineAttribute[resend.EventReference]("resend-event-created")

// Flow creates one Resend draft event and completes with its ID.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the verification result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the created event ID.
// dex:field attribute-key:resend-event-created value-type:json editable:false description:"Created event"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readEventOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-event-created": value}}, nil
}

// GetDexDisplay returns the created event ID.
// dex:field attribute-key:resend-event-created value-type:json editable:false description:"Created event"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readEventOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-event-created": value}}, nil
}

func readEventOutcome(ctx dex.Context) (resend.EventReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.EventReference{}, nil
	}
	return value, err
}

// GetSteps registers the event creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareEvent{}),
		dex.DefineStep(resend.NewCreateEventStep(resend.CreateEventStepConfig[Request]{
			StepType: "CreateEvent", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-event", GroupLabel: "Resend event", Explanation: "Create the selected Resend draft event."},
			MapToOperationInput: func(request Request) resend.CreateEventInput { return resend.CreateEventInput(request) },
			Created:             sdkgo.GoTo(completeEvent{}),
		})),
		dex.DefineStep(completeEvent{}),
	}
}

// dex:group group-id:resend-event-prepare group-label:"Prepare event"
// dex:explanation text:"Pass the typed event request into the provider create."
type prepareEvent struct{ dex.StepDefaults }

func (prepareEvent) GetStepType() string { return "PrepareEvent" }
func (prepareEvent) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareEvent) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateEvent"), request), nil
}

// dex:group group-id:resend-event-complete group-label:"Complete event creation"
// dex:explanation text:"Complete after Resend creates the event."
type completeEvent struct {
	dex.StepDefaultsNoWaitFor[resend.CreateEventResult]
}

func (completeEvent) GetStepType() string { return "CompleteEvent" }
func (completeEvent) Execute(ctx dex.Context, result resend.CreateEventResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
