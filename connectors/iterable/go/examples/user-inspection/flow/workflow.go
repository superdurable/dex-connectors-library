// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package userinspection runs Iterable getUser as one Dex Flow.
package userinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/iterable/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "IterableUserInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "iterable"
	operationStepType = "CallGetUser"
)

var userAttribute = dex.DefineAttribute[iterable.UserIdentity]("iterable-user")

// Flow runs GetUser once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection iterable.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection iterable.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(iterable.NewGetUserStep(iterable.GetUserStepConfig[iterable.GetUserInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Iterable", Explanation: "Read a user from Iterable."},
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

// dex:field attribute-key:iterable-user value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"iterable-user": outcome}}, nil
}

// dex:field attribute-key:iterable-user value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"iterable-user": outcome}}, nil
}

func readUser(ctx dex.Context) (iterable.UserIdentity, error) {
	outcome, err := userAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return iterable.UserIdentity{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input iterable.GetUserInput) iterable.GetUserInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetUser" }
func (startRequest) WaitFor(dex.Context, iterable.GetUserInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input iterable.GetUserInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[iterable.GetUserInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[iterable.GetUserResult]
}

func (completeResult) GetStepType() string { return "CompleteGetUser" }
func (completeResult) Execute(ctx dex.Context, result iterable.GetUserResult) (*dex.StepDecision, error) {
	if err := userAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
