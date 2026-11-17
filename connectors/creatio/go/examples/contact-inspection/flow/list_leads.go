// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listLeadsStepType = "ListCreatioLeads"

var leadsPageAttribute = dex.DefineAttribute[creatio.LeadsPage]("creatio-leads-page")

// LeadsSearchFlow reads one bounded page of Creatio leads.
type LeadsSearchFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewLeadsSearchFlow binds the static Creatio connection.
func NewLeadsSearchFlow(connection creatio.Connection) *LeadsSearchFlow {
	return &LeadsSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*LeadsSearchFlow) GetFlowType() string { return "CreatioLeadSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*LeadsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadsPageAttribute}}
}

// GetSteps wires the operation-specific leads Query.
func (flow *LeadsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadsSearch{}),
		dex.DefineStep(creatio.NewListLeadsStep(creatio.ListLeadsStepConfig[creatio.ListLeadsInput]{
			StepType: listLeadsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Read one page of Creatio leads."},
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

// dex:field attribute-key:creatio-leads-page value-type:json editable:false description:"Leads search page"
// GetDexSummary shows the returned page in Dex Web.
func (*LeadsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-leads-page": page}}, nil
}

// dex:field attribute-key:creatio-leads-page value-type:json editable:false description:"Leads search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*LeadsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readLeadsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-leads-page": page}}, nil
}

func readLeadsPage(ctx dex.Context) (creatio.LeadsPage, error) {
	page, err := leadsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.LeadsPage{}, nil
	}
	return page, err
}

func mapLeadsSearchInput(input creatio.ListLeadsInput) creatio.ListLeadsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested leads page and filters to Creatio."
type startLeadsSearch struct{ dex.StepDefaults }

func (startLeadsSearch) WaitFor(dex.Context, creatio.ListLeadsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadsSearch) Execute(_ dex.Context, input creatio.ListLeadsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.ListLeadsInput](listLeadsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the leads page and complete the Flow."
type completeLeadsSearch struct {
	dex.StepDefaultsNoWaitFor[creatio.ListLeadsResult]
}

func (completeLeadsSearch) Execute(ctx dex.Context, result creatio.ListLeadsResult) (*dex.StepDecision, error) {
	if err := leadsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadsSearchFlow)(nil)
