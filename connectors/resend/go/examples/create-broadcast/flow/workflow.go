// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package createbroadcast demonstrates Resend draft broadcast creation.
package createbroadcast

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendCreateBroadcast"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the new broadcast settings.
type Request resend.CreateBroadcastInput

var outcomeAttribute = dex.DefineAttribute[resend.BroadcastReference]("resend-broadcast-created")

// Flow creates one Resend draft broadcast and completes with its ID.
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

// GetDexSummary returns the created broadcast ID.
// dex:field attribute-key:resend-broadcast-created value-type:json editable:false description:"Created broadcast"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readBroadcastOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-broadcast-created": value}}, nil
}

// GetDexDisplay returns the created broadcast ID.
// dex:field attribute-key:resend-broadcast-created value-type:json editable:false description:"Created broadcast"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readBroadcastOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-broadcast-created": value}}, nil
}

func readBroadcastOutcome(ctx dex.Context) (resend.BroadcastReference, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.BroadcastReference{}, nil
	}
	return value, err
}

// GetSteps registers the broadcast creation path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareBroadcast{}),
		dex.DefineStep(resend.NewCreateBroadcastStep(resend.CreateBroadcastStepConfig[Request]{
			StepType: "CreateBroadcast", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-broadcast", GroupLabel: "Resend broadcast", Explanation: "Create the selected Resend draft broadcast."},
			MapToOperationInput: func(request Request) resend.CreateBroadcastInput { return resend.CreateBroadcastInput(request) },
			Created:             sdkgo.GoTo(completeBroadcast{}),
		})),
		dex.DefineStep(completeBroadcast{}),
	}
}

// dex:group group-id:resend-broadcast-prepare group-label:"Prepare broadcast"
// dex:explanation text:"Pass the typed broadcast request into the provider create."
type prepareBroadcast struct{ dex.StepDefaults }

func (prepareBroadcast) GetStepType() string { return "PrepareBroadcast" }
func (prepareBroadcast) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareBroadcast) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("CreateBroadcast"), request), nil
}

// dex:group group-id:resend-broadcast-complete group-label:"Complete broadcast creation"
// dex:explanation text:"Complete after Resend creates the broadcast."
type completeBroadcast struct {
	dex.StepDefaultsNoWaitFor[resend.CreateBroadcastResult]
}

func (completeBroadcast) GetStepType() string { return "CompleteBroadcast" }
func (completeBroadcast) Execute(ctx dex.Context, result resend.CreateBroadcastResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
