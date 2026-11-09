// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package profileinspection runs Braze getUserProfile as one Dex Flow.
package profileinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/braze/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BrazeProfileInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "braze"
	operationStepType = "CallGetUserProfile"
)

var profileAttribute = dex.DefineAttribute[braze.UserProfile]("braze-profile")

// Flow runs GetUserProfile once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection braze.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection braze.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{profileAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(braze.NewGetUserProfileStep(braze.GetUserProfileStepConfig[braze.GetUserProfileInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Braze", Explanation: "Read a profile from Braze."},
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

// dex:field attribute-key:braze-profile value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braze-profile": outcome}}, nil
}

// dex:field attribute-key:braze-profile value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"braze-profile": outcome}}, nil
}

func readProfile(ctx dex.Context) (braze.UserProfile, error) {
	outcome, err := profileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return braze.UserProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input braze.GetUserProfileInput) braze.GetUserProfileInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetUserProfile" }
func (startRequest) WaitFor(dex.Context, braze.GetUserProfileInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input braze.GetUserProfileInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[braze.GetUserProfileInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[braze.GetUserProfileResult]
}

func (completeResult) GetStepType() string { return "CompleteGetUserProfile" }
func (completeResult) Execute(ctx dex.Context, result braze.GetUserProfileResult) (*dex.StepDecision, error) {
	if err := profileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
