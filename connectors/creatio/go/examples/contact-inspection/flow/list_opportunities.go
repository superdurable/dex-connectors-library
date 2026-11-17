// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listOpportunitiesStepType = "ListCreatioOpportunities"

var opportunitiesPageAttribute = dex.DefineAttribute[creatio.OpportunitiesPage]("creatio-opportunities-page")

// OpportunitiesSearchFlow reads one bounded page of Creatio opportunities.
type OpportunitiesSearchFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewOpportunitiesSearchFlow binds the static Creatio connection.
func NewOpportunitiesSearchFlow(connection creatio.Connection) *OpportunitiesSearchFlow {
	return &OpportunitiesSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*OpportunitiesSearchFlow) GetFlowType() string { return "CreatioOpportunitySearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*OpportunitiesSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{opportunitiesPageAttribute}}
}

// GetSteps wires the operation-specific opportunities Query.
func (flow *OpportunitiesSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startOpportunitiesSearch{}),
		dex.DefineStep(creatio.NewListOpportunitiesStep(creatio.ListOpportunitiesStepConfig[creatio.ListOpportunitiesInput]{
			StepType: listOpportunitiesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Read one page of Creatio opportunities."},
			Connection:  flow.connection, MapToOperationInput: mapOpportunitiesSearchInput,
			Listed: sdkgo.GoTo(completeOpportunitiesSearch{}),
		})),
		dex.DefineStep(completeOpportunitiesSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *OpportunitiesSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-opportunities-page value-type:json editable:false description:"Opportunities search page"
// GetDexSummary shows the returned page in Dex Web.
func (*OpportunitiesSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunitiesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-opportunities-page": page}}, nil
}

// dex:field attribute-key:creatio-opportunities-page value-type:json editable:false description:"Opportunities search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*OpportunitiesSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readOpportunitiesPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-opportunities-page": page}}, nil
}

func readOpportunitiesPage(ctx dex.Context) (creatio.OpportunitiesPage, error) {
	page, err := opportunitiesPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.OpportunitiesPage{}, nil
	}
	return page, err
}

func mapOpportunitiesSearchInput(input creatio.ListOpportunitiesInput) creatio.ListOpportunitiesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested opportunities page and filters to Creatio."
type startOpportunitiesSearch struct{ dex.StepDefaults }

func (startOpportunitiesSearch) WaitFor(dex.Context, creatio.ListOpportunitiesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startOpportunitiesSearch) Execute(_ dex.Context, input creatio.ListOpportunitiesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.ListOpportunitiesInput](listOpportunitiesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the opportunities page and complete the Flow."
type completeOpportunitiesSearch struct {
	dex.StepDefaultsNoWaitFor[creatio.ListOpportunitiesResult]
}

func (completeOpportunitiesSearch) Execute(ctx dex.Context, result creatio.ListOpportunitiesResult) (*dex.StepDecision, error) {
	if err := opportunitiesPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*OpportunitiesSearchFlow)(nil)
