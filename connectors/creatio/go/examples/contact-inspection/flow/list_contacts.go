// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listContactsStepType = "ListCreatioContacts"

var contactsPageAttribute = dex.DefineAttribute[creatio.ContactsPage]("creatio-contacts-page")

// ContactsSearchFlow reads one bounded page of Creatio contacts.
type ContactsSearchFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewContactsSearchFlow binds the static Creatio connection.
func NewContactsSearchFlow(connection creatio.Connection) *ContactsSearchFlow {
	return &ContactsSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ContactsSearchFlow) GetFlowType() string { return "CreatioContactSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*ContactsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactsPageAttribute}}
}

// GetSteps wires the operation-specific contacts Query.
func (flow *ContactsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactsSearch{}),
		dex.DefineStep(creatio.NewListContactsStep(creatio.ListContactsStepConfig[creatio.ListContactsInput]{
			StepType: listContactsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Read one page of Creatio contacts."},
			Connection:  flow.connection, MapToOperationInput: mapContactsSearchInput,
			Listed: sdkgo.GoTo(completeContactsSearch{}),
		})),
		dex.DefineStep(completeContactsSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *ContactsSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-contacts-page value-type:json editable:false description:"Contacts search page"
// GetDexSummary shows the returned page in Dex Web.
func (*ContactsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-contacts-page": page}}, nil
}

// dex:field attribute-key:creatio-contacts-page value-type:json editable:false description:"Contacts search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*ContactsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-contacts-page": page}}, nil
}

func readContactsPage(ctx dex.Context) (creatio.ContactsPage, error) {
	page, err := contactsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.ContactsPage{}, nil
	}
	return page, err
}

func mapContactsSearchInput(input creatio.ListContactsInput) creatio.ListContactsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested contacts page and filters to Creatio."
type startContactsSearch struct{ dex.StepDefaults }

func (startContactsSearch) WaitFor(dex.Context, creatio.ListContactsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactsSearch) Execute(_ dex.Context, input creatio.ListContactsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.ListContactsInput](listContactsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the contacts page and complete the Flow."
type completeContactsSearch struct {
	dex.StepDefaultsNoWaitFor[creatio.ListContactsResult]
}

func (completeContactsSearch) Execute(ctx dex.Context, result creatio.ListContactsResult) (*dex.StepDecision, error) {
	if err := contactsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactsSearchFlow)(nil)
