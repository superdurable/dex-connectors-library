// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package userread runs Easemob getUserDetail as one Dex Flow.
package userread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/easemob/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EasemobUserRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "easemob"
	operationStepType = "CallGetUserDetail"
)

var userAttribute = dex.DefineAttribute[easemob.UserDetail]("easemob-user-read")

// Flow runs GetUserDetail once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection easemob.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection easemob.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(easemob.NewGetUserDetailStep(easemob.GetUserDetailStepConfig[easemob.GetUserDetailInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Easemob", Explanation: "Read one Easemob IM user."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:easemob-user-read value-type:json editable:false description:"Easemob user detail"
// GetDexSummary shows the last Easemob user detail in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"easemob-user-read": outcome}}, nil
}

// dex:field attribute-key:easemob-user-read value-type:json editable:false description:"Easemob user detail"
// GetDexDisplay shows the last Easemob user detail in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"easemob-user-read": outcome}}, nil
}

func readUser(ctx dex.Context) (easemob.UserDetail, error) {
	outcome, err := userAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return easemob.UserDetail{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input easemob.GetUserDetailInput) easemob.GetUserDetailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetUserDetail" }
func (startRequest) WaitFor(dex.Context, easemob.GetUserDetailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input easemob.GetUserDetailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[easemob.GetUserDetailInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[easemob.GetUserDetailResult]
}

func (completeResult) GetStepType() string { return "CompleteGetUserDetail" }
func (completeResult) Execute(ctx dex.Context, result easemob.GetUserDetailResult) (*dex.StepDecision, error) {
	if err := userAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
