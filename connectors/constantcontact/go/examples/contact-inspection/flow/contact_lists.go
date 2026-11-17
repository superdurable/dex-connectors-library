// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const contactListsStepType = "ListConstantContactLists"

// ContactListsFlow reads one page of lists and completes with its typed result.
type ContactListsFlow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewContactListsFlow binds the provider connection for list inspection.
func NewContactListsFlow(connection constantcontact.Connection) *ContactListsFlow {
	return &ContactListsFlow{connection: connection}
}

// GetPersistenceSchema declares the list result retained for Dex Web.
func (*ContactListsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactListsAttribute}}
}

// GetSteps wires the operation-specific list Query to a completion Step.
func (flow *ContactListsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactLists{}),
		dex.DefineStep(constantcontact.NewListContactListsStep(constantcontact.ListContactListsStepConfig[constantcontact.ListContactListsInput]{
			StepType: contactListsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Read a page of contact lists from Constant Contact."},
			Connection:          flow.connection,
			MapToOperationInput: mapContactListsInput,
			Listed:              sdkgo.GoTo(completeContactLists{}),
		})),
		dex.DefineStep(completeContactLists{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the last list page.
func (flow *ContactListsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:constantcontact-contact-lists value-type:json editable:false description:"Provider list page"
// GetDexSummary shows the last list page in Dex Web.
func (*ContactListsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactLists(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact-lists": page}}, nil
}

// dex:field attribute-key:constantcontact-contact-lists value-type:json editable:false description:"Provider list page"
// GetDexDisplay shows the last list page in Dex Web.
func (*ContactListsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactLists(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact-lists": page}}, nil
}

func readContactLists(ctx dex.Context) (constantcontact.ContactListsPage, error) {
	page, err := contactListsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.ContactListsPage{}, nil
	}
	return page, err
}

var contactListsAttribute = dex.DefineAttribute[constantcontact.ContactListsPage]("constantcontact-contact-lists")

func mapContactListsInput(input constantcontact.ListContactListsInput) constantcontact.ListContactListsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested contact list page to the provider Step."
type startContactLists struct{ dex.StepDefaults }

func (startContactLists) WaitFor(dex.Context, constantcontact.ListContactListsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactLists) Execute(_ dex.Context, input constantcontact.ListContactListsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.ListContactListsInput](contactListsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned contact list page and complete the Flow."
type completeContactLists struct {
	dex.StepDefaultsNoWaitFor[constantcontact.ListContactListsResult]
}

func (completeContactLists) Execute(ctx dex.Context, result constantcontact.ListContactListsResult) (*dex.StepDecision, error) {
	if err := contactListsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContactListsFlow)(nil)
