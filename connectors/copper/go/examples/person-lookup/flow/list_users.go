// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listUsersStepType = "ListCopperUsers"

var usersPageAttribute = dex.DefineAttribute[copper.UsersPage]("copper-users-page")

// UsersSearchFlow reads one bounded page of Copper users.
type UsersSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewUsersSearchFlow binds the static Copper connection.
func NewUsersSearchFlow(connection copper.Connection) *UsersSearchFlow {
	return &UsersSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*UsersSearchFlow) GetFlowType() string { return "CopperUserSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*UsersSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{usersPageAttribute}}
}

// GetSteps wires the operation-specific users Query.
func (flow *UsersSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUsersSearch{}),
		dex.DefineStep(copper.NewListUsersStep(copper.ListUsersStepConfig[copper.ListUsersInput]{
			StepType: listUsersStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper users."},
			Connection:  flow.connection, MapToOperationInput: mapUsersSearchInput,
			Listed: sdkgo.GoTo(completeUsersSearch{}),
		})),
		dex.DefineStep(completeUsersSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *UsersSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-users-page value-type:json editable:false description:"Users search page"
// GetDexSummary shows the returned page in Dex Web.
func (*UsersSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readUsersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-users-page": page}}, nil
}

// dex:field attribute-key:copper-users-page value-type:json editable:false description:"Users search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*UsersSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readUsersPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-users-page": page}}, nil
}

func readUsersPage(ctx dex.Context) (copper.UsersPage, error) {
	page, err := usersPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.UsersPage{}, nil
	}
	return page, err
}

func mapUsersSearchInput(input copper.ListUsersInput) copper.ListUsersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested users page and filters to Copper."
type startUsersSearch struct{ dex.StepDefaults }

func (startUsersSearch) WaitFor(dex.Context, copper.ListUsersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUsersSearch) Execute(_ dex.Context, input copper.ListUsersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListUsersInput](listUsersStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the users page and complete the Flow."
type completeUsersSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListUsersResult]
}

func (completeUsersSearch) Execute(ctx dex.Context, result copper.ListUsersResult) (*dex.StepDecision, error) {
	if err := usersPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UsersSearchFlow)(nil)
