// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UserListFlow lists user role metadata for one Lever opportunity.
type UserListFlow struct {
	dex.FlowDefaults
	connection lever.Connection
}

// NewUserListFlow binds the provider connection at registration time.
func NewUserListFlow(connection lever.Connection) *UserListFlow {
	return &UserListFlow{connection: connection}
}
func (*UserListFlow) GetFlowType() string { return "LeverUserList" }

var usersAttribute = dex.DefineAttribute[lever.UsersPage]("lever-users")

func (*UserListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{usersAttribute}}
}
func (flow *UserListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUserList{}),
		dex.DefineStep(lever.NewListUsersStep(lever.ListUsersStepConfig[lever.ListUsersInput]{
			StepType: "CallListUsers", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lever", Explanation: "List users for the selected opportunity."},
			Connection:  flow.connection, MapToOperationInput: mapUserListInput,
			Listed: sdkgo.GoTo(completeUserList{}),
		})),
		dex.DefineStep(completeUserList{}),
	}
}
func (flow *UserListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lever-users value-type:json editable:false description:"User role metadata"
// GetDexSummary shows the user page in Dex Web.
func (*UserListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readUsers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-users": value}}, nil
}

// dex:field attribute-key:lever-users value-type:json editable:false description:"User role metadata"
// GetDexDisplay shows the user page in Dex Web.
func (*UserListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readUsers(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-users": value}}, nil
}
func readUsers(ctx dex.Context) (lever.UsersPage, error) {
	value, err := usersAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lever.UsersPage{}, nil
	}
	return value, err
}
func mapUserListInput(input lever.ListUsersInput) lever.ListUsersInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the opportunity ID to the provider Step."
type startUserList struct{ dex.StepDefaults }

func (startUserList) GetStepType() string { return "StartListUsers" }
func (startUserList) WaitFor(dex.Context, lever.ListUsersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUserList) Execute(_ dex.Context, input lever.ListUsersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lever.ListUsersInput]("CallListUsers"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the user role metadata and complete the Flow."
type completeUserList struct {
	dex.StepDefaultsNoWaitFor[lever.ListUsersResult]
}

func (completeUserList) GetStepType() string { return "CompleteListUsers" }
func (completeUserList) Execute(ctx dex.Context, result lever.ListUsersResult) (*dex.StepDecision, error) {
	if err := usersAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UserListFlow)(nil)
