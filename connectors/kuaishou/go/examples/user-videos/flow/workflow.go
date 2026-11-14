// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package uservideos runs Kuaishou listUserVideos as one Dex Flow.
package uservideos

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kuaishou/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "KuaishouUserVideos"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kuaishou"
	operationStepType = "CallListUserVideos"
)

var userAttribute = dex.DefineAttribute[kuaishou.UserVideosPage]("kuaishou-user-videos")

// Flow runs ListUserVideos once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kuaishou.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kuaishou.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kuaishou.NewListUserVideosStep(kuaishou.ListUserVideosStepConfig[kuaishou.ListUserVideosInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kuaishou", Explanation: "Read the authorized Kuaishou video page."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kuaishou-user-videos value-type:json editable:false description:"Kuaishou video page information"
// GetDexSummary shows the last Kuaishou video page information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kuaishou-user-videos": outcome}}, nil
}

// dex:field attribute-key:kuaishou-user-videos value-type:json editable:false description:"Kuaishou video page information"
// GetDexDisplay shows the last Kuaishou video page information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kuaishou-user-videos": outcome}}, nil
}

func readUser(ctx dex.Context) (kuaishou.UserVideosPage, error) {
	outcome, err := userAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kuaishou.UserVideosPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input kuaishou.ListUserVideosInput) kuaishou.ListUserVideosInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListUserVideos" }
func (startRequest) WaitFor(dex.Context, kuaishou.ListUserVideosInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kuaishou.ListUserVideosInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kuaishou.ListUserVideosInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kuaishou.ListUserVideosResult]
}

func (completeResult) GetStepType() string { return "CompleteListUserVideos" }
func (completeResult) Execute(ctx dex.Context, result kuaishou.ListUserVideosResult) (*dex.StepDecision, error) {
	if err := userAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
