// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package usersearch runs Amplitude searchUsers as one Dex Flow.
package usersearch

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/amplitude/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AmplitudeUserSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "amplitude"
	operationStepType = "CallSearchUsers"
)

var usersAttribute = dex.DefineAttribute[amplitude.UserSearchPage]("amplitude-user-search")

// Flow runs SearchUsers once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection amplitude.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection amplitude.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{usersAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(amplitude.NewSearchUsersStep(amplitude.SearchUsersStepConfig[amplitude.SearchUsersInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Amplitude", Explanation: "Search users in Amplitude."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Searched:            sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:amplitude-user-search value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUsers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-user-search": outcome}}, nil
}

// dex:field attribute-key:amplitude-user-search value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUsers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"amplitude-user-search": outcome}}, nil
}

func readUsers(ctx dex.Context) (amplitude.UserSearchPage, error) {
	outcome, err := usersAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return amplitude.UserSearchPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input amplitude.SearchUsersInput) amplitude.SearchUsersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchUsers" }
func (startRequest) WaitFor(dex.Context, amplitude.SearchUsersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input amplitude.SearchUsersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[amplitude.SearchUsersInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[amplitude.SearchUsersResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchUsers" }
func (completeResult) Execute(ctx dex.Context, result amplitude.SearchUsersResult) (*dex.StepDecision, error) {
	if err := usersAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
