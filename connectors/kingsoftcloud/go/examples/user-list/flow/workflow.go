// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package userlist runs Kingsoft Cloud listUsers as one Dex Flow.
package userlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/kingsoftcloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "KingsoftCloudUserList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "kingsoftcloud"
	operationStepType = "CallListUsers"
)

var userListOutcomeAttribute = dex.DefineAttribute[kingsoftcloud.ListUsersOutput]("kingsoftcloud-user-list-outcome")

// Flow runs ListUsers once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection kingsoftcloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection kingsoftcloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(kingsoftcloud.NewListUsersStep(kingsoftcloud.ListUsersStepConfig[kingsoftcloud.ListUsersInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kingsoft Cloud", Explanation: "List one IAM user page by calling the signed provider operation."},
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

// dex:field attribute-key:kingsoftcloud-user-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kingsoftcloud-user-list-outcome": outcome}}, nil
}

// dex:field attribute-key:kingsoftcloud-user-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUserListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kingsoftcloud-user-list-outcome": outcome}}, nil
}

func readUserListOutcome(ctx dex.Context) (kingsoftcloud.ListUsersOutput, error) {
	outcome, err := userListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kingsoftcloud.ListUsersOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input kingsoftcloud.ListUsersInput) kingsoftcloud.ListUsersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListUsers" }
func (startRequest) WaitFor(dex.Context, kingsoftcloud.ListUsersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input kingsoftcloud.ListUsersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kingsoftcloud.ListUsersInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[kingsoftcloud.ListUsersResult]
}

func (completeResult) GetStepType() string { return "CompleteListUsers" }
func (completeResult) Execute(ctx dex.Context, result kingsoftcloud.ListUsersResult) (*dex.StepDecision, error) {
	if err := userListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
