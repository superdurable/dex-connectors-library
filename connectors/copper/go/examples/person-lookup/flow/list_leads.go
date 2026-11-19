// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listLeadsStepType = "ListCopperLeads"

var leadsPageAttribute = dex.DefineAttribute[copper.LeadsPage]("copper-leads-page")

// LeadsSearchFlow reads one bounded page of Copper leads.
type LeadsSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewLeadsSearchFlow binds the static Copper connection.
func NewLeadsSearchFlow(connection copper.Connection) *LeadsSearchFlow {
	return &LeadsSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*LeadsSearchFlow) GetFlowType() string { return "CopperLeadSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*LeadsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadsPageAttribute}}
}

// GetSteps wires the operation-specific leads Query.
func (flow *LeadsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadsSearch{}),
		dex.DefineStep(copper.NewListLeadsStep(copper.ListLeadsStepConfig[copper.ListLeadsInput]{
			StepType: listLeadsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper leads."},
			Connection:  flow.connection, MapToOperationInput: mapLeadsSearchInput,
			Listed: sdkgo.GoTo(completeLeadsSearch{}),
		})),
		dex.DefineStep(completeLeadsSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *LeadsSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-leads-page value-type:json editable:false description:"Leads search page"
// GetDexSummary shows the returned page in Dex Web.
func (*LeadsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-leads-page": page}}, nil
}

// dex:field attribute-key:copper-leads-page value-type:json editable:false description:"Leads search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*LeadsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-leads-page": page}}, nil
}

func readLeadsPage(ctx dex.Context) (copper.LeadsPage, error) {
	page, err := leadsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.LeadsPage{}, nil
	}
	return page, err
}

func mapLeadsSearchInput(input copper.ListLeadsInput) copper.ListLeadsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested leads page and filters to Copper."
type startLeadsSearch struct{ dex.StepDefaults }

func (startLeadsSearch) WaitFor(dex.Context, copper.ListLeadsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadsSearch) Execute(_ dex.Context, input copper.ListLeadsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListLeadsInput](listLeadsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the leads page and complete the Flow."
type completeLeadsSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListLeadsResult]
}

func (completeLeadsSearch) Execute(ctx dex.Context, result copper.ListLeadsResult) (*dex.StepDecision, error) {
	if err := leadsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadsSearchFlow)(nil)
