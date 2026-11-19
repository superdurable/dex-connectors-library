// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listPeopleStepType = "ListCopperPeople"

var peoplePageAttribute = dex.DefineAttribute[copper.PeoplePage]("copper-people-page")

// PeopleSearchFlow reads one bounded page of Copper people.
type PeopleSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewPeopleSearchFlow binds the static Copper connection.
func NewPeopleSearchFlow(connection copper.Connection) *PeopleSearchFlow {
	return &PeopleSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*PeopleSearchFlow) GetFlowType() string { return "CopperPeopleSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*PeopleSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{peoplePageAttribute}}
}

// GetSteps wires the operation-specific people Query.
func (flow *PeopleSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPeopleSearch{}),
		dex.DefineStep(copper.NewListPeopleStep(copper.ListPeopleStepConfig[copper.ListPeopleInput]{
			StepType: listPeopleStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper people."},
			Connection:  flow.connection, MapToOperationInput: mapPeopleSearchInput,
			Listed: sdkgo.GoTo(completePeopleSearch{}),
		})),
		dex.DefineStep(completePeopleSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *PeopleSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-people-page value-type:json editable:false description:"People search page"
// GetDexSummary shows the returned page in Dex Web.
func (*PeopleSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPeoplePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-people-page": page}}, nil
}

// dex:field attribute-key:copper-people-page value-type:json editable:false description:"People search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*PeopleSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readPeoplePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-people-page": page}}, nil
}

func readPeoplePage(ctx dex.Context) (copper.PeoplePage, error) {
	page, err := peoplePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.PeoplePage{}, nil
	}
	return page, err
}

func mapPeopleSearchInput(input copper.ListPeopleInput) copper.ListPeopleInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested people page and filters to Copper."
type startPeopleSearch struct{ dex.StepDefaults }

func (startPeopleSearch) WaitFor(dex.Context, copper.ListPeopleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPeopleSearch) Execute(_ dex.Context, input copper.ListPeopleInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListPeopleInput](listPeopleStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the people page and complete the Flow."
type completePeopleSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListPeopleResult]
}

func (completePeopleSearch) Execute(ctx dex.Context, result copper.ListPeopleResult) (*dex.StepDecision, error) {
	if err := peoplePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PeopleSearchFlow)(nil)
