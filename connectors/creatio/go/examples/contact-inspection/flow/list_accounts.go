// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listAccountsStepType = "ListCreatioAccounts"

var accountsPageAttribute = dex.DefineAttribute[creatio.AccountsPage]("creatio-accounts-page")

// AccountsSearchFlow reads one bounded page of Creatio accounts.
type AccountsSearchFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewAccountsSearchFlow binds the static Creatio connection.
func NewAccountsSearchFlow(connection creatio.Connection) *AccountsSearchFlow {
	return &AccountsSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*AccountsSearchFlow) GetFlowType() string { return "CreatioAccountSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*AccountsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountsPageAttribute}}
}

// GetSteps wires the operation-specific accounts Query.
func (flow *AccountsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAccountsSearch{}),
		dex.DefineStep(creatio.NewListAccountsStep(creatio.ListAccountsStepConfig[creatio.ListAccountsInput]{
			StepType: listAccountsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Read one page of Creatio accounts."},
			Connection:  flow.connection, MapToOperationInput: mapAccountsSearchInput,
			Listed: sdkgo.GoTo(completeAccountsSearch{}),
		})),
		dex.DefineStep(completeAccountsSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *AccountsSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-accounts-page value-type:json editable:false description:"Accounts search page"
// GetDexSummary shows the returned page in Dex Web.
func (*AccountsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAccountsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-accounts-page": page}}, nil
}

// dex:field attribute-key:creatio-accounts-page value-type:json editable:false description:"Accounts search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*AccountsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAccountsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-accounts-page": page}}, nil
}

func readAccountsPage(ctx dex.Context) (creatio.AccountsPage, error) {
	page, err := accountsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.AccountsPage{}, nil
	}
	return page, err
}

func mapAccountsSearchInput(input creatio.ListAccountsInput) creatio.ListAccountsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested accounts page and filters to Creatio."
type startAccountsSearch struct{ dex.StepDefaults }

func (startAccountsSearch) WaitFor(dex.Context, creatio.ListAccountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAccountsSearch) Execute(_ dex.Context, input creatio.ListAccountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.ListAccountsInput](listAccountsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the accounts page and complete the Flow."
type completeAccountsSearch struct {
	dex.StepDefaultsNoWaitFor[creatio.ListAccountsResult]
}

func (completeAccountsSearch) Execute(ctx dex.Context, result creatio.ListAccountsResult) (*dex.StepDecision, error) {
	if err := accountsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AccountsSearchFlow)(nil)
