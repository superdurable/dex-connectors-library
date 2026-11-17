// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createsegment demonstrates Resend contact segment creation.
package createsegment

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateSegment"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new segment settings.
type Request resend.CreateSegmentInput

var outcomeAttribute = dex.DefineAttribute[resend.SegmentReference]("resend-segment-created")

// Flow creates one Resend contact segment and completes with its ID.
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

// GetDexSummary returns the created segment ID.
// dex:field attribute-key:resend-segment-created value-type:json editable:false description:"Created segment"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSegmentOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-segment-created": value}}, nil
}

// GetDexDisplay returns the created segment ID.
// dex:field attribute-key:resend-segment-created value-type:json editable:false description:"Created segment"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readSegmentOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-segment-created": value}}, nil
}

func readSegmentOutcome(ctx dex.Context) (resend.SegmentReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.SegmentReference{}, nil
	}
	return value, err
}

// GetSteps registers the segment creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareSegment{}),
		dex.DefineStep(resend.NewCreateSegmentStep(resend.CreateSegmentStepConfig[Request]{
			StepType: "CreateSegment", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-segment", GroupLabel: "Resend segment", Explanation: "Create the selected Resend contact segment."},
			MapToOperationInput: func(request Request) resend.CreateSegmentInput { return resend.CreateSegmentInput(request) },
			Created:             sdkgo.GoTo(completeSegment{}),
		})),
		dex.DefineStep(completeSegment{}),
	}
}

// dex:group group-id:resend-segment-prepare group-label:"Prepare segment"
// dex:explanation text:"Pass the typed segment request into the provider create."
type prepareSegment struct{ dex.StepDefaults }

func (prepareSegment) GetStepType() string { return "PrepareSegment" }
func (prepareSegment) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareSegment) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateSegment"), request), nil
}

// dex:group group-id:resend-segment-complete group-label:"Complete segment creation"
// dex:explanation text:"Complete after Resend creates the segment."
type completeSegment struct {
	dex.StepDefaultsNoWaitFor[resend.CreateSegmentResult]
}

func (completeSegment) GetStepType() string { return "CompleteSegment" }
func (completeSegment) Execute(ctx dex.Context, result resend.CreateSegmentResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
