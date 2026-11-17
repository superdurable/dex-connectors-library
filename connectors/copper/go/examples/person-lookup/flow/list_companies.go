// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listCompaniesStepType = "ListCopperCompanies"

var companiesPageAttribute = dex.DefineAttribute[copper.CompaniesPage]("copper-companies-page")

// CompaniesSearchFlow reads one bounded page of Copper companies.
type CompaniesSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewCompaniesSearchFlow binds the static Copper connection.
func NewCompaniesSearchFlow(connection copper.Connection) *CompaniesSearchFlow {
	return &CompaniesSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*CompaniesSearchFlow) GetFlowType() string { return "CopperCompanySearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*CompaniesSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companiesPageAttribute}}
}

// GetSteps wires the operation-specific companies Query.
func (flow *CompaniesSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCompaniesSearch{}),
		dex.DefineStep(copper.NewListCompaniesStep(copper.ListCompaniesStepConfig[copper.ListCompaniesInput]{
			StepType: listCompaniesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper companies."},
			Connection:  flow.connection, MapToOperationInput: mapCompaniesSearchInput,
			Listed: sdkgo.GoTo(completeCompaniesSearch{}),
		})),
		dex.DefineStep(completeCompaniesSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *CompaniesSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-companies-page value-type:json editable:false description:"Companies search page"
// GetDexSummary shows the returned page in Dex Web.
func (*CompaniesSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCompaniesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-companies-page": page}}, nil
}

// dex:field attribute-key:copper-companies-page value-type:json editable:false description:"Companies search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*CompaniesSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCompaniesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-companies-page": page}}, nil
}

func readCompaniesPage(ctx dex.Context) (copper.CompaniesPage, error) {
	page, err := companiesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.CompaniesPage{}, nil
	}
	return page, err
}

func mapCompaniesSearchInput(input copper.ListCompaniesInput) copper.ListCompaniesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested companies page and filters to Copper."
type startCompaniesSearch struct{ dex.StepDefaults }

func (startCompaniesSearch) WaitFor(dex.Context, copper.ListCompaniesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCompaniesSearch) Execute(_ dex.Context, input copper.ListCompaniesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListCompaniesInput](listCompaniesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the companies page and complete the Flow."
type completeCompaniesSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListCompaniesResult]
}

func (completeCompaniesSearch) Execute(ctx dex.Context, result copper.ListCompaniesResult) (*dex.StepDecision, error) {
	if err := companiesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CompaniesSearchFlow)(nil)
