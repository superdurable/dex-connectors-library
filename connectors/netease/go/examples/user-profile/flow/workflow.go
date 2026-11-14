// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package userprofile runs NetEase Yunxin getUserProfile as one Dex Flow.
package userprofile

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/netease/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "NetEaseYunxinUserProfile"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "netease"
	operationStepType = "CallGetUserProfile"
)

var userProfileOutcomeAttribute = dex.DefineAttribute[netease.GetUserProfileOutput]("netease-user-profile-outcome")

// Flow runs GetUserProfile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection netease.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection netease.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userProfileOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(netease.NewGetUserProfileStep(netease.GetUserProfileStepConfig[netease.GetUserProfileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "NetEase Yunxin", Explanation: "Read the requested IM user profile."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:netease-user-profile-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserProfileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"netease-user-profile-outcome": outcome}}, nil
}

// dex:field attribute-key:netease-user-profile-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserProfileOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"netease-user-profile-outcome": outcome}}, nil
}

func readUserProfileOutcome(ctx dex.Context) (netease.GetUserProfileOutput, error) {
	outcome, err := userProfileOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return netease.GetUserProfileOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input netease.GetUserProfileInput) netease.GetUserProfileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetUserProfile" }
func (startRequest) WaitFor(dex.Context, netease.GetUserProfileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input netease.GetUserProfileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[netease.GetUserProfileInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[netease.GetUserProfileResult]
}

func (completeResult) GetStepType() string { return "CompleteGetUserProfile" }
func (completeResult) Execute(ctx dex.Context, result netease.GetUserProfileResult) (*dex.StepDecision, error) {
	if err := userProfileOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
