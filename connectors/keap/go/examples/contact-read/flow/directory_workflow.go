// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// DirectoryFlowType is the contact directory example Flow identity.
const DirectoryFlowType = "KeapContactDirectory"

const listContactsStepType = "CallListContacts"

var contactsPageAttribute = dex.DefineAttribute[keap.ContactsPage]("keap-contacts-page")

// DirectoryFlow lists one bounded page of contacts.
type DirectoryFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewDirectoryFlow binds the Keap connection at registration time.
func NewDirectoryFlow(connection keap.Connection) *DirectoryFlow {
	return &DirectoryFlow{connection: connection}
}

func (*DirectoryFlow) GetFlowType() string { return DirectoryFlowType }

func (*DirectoryFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactsPageAttribute}}
}

func (flow *DirectoryFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startDirectory{}),
		dex.DefineStep(keap.NewListContactsStep(keap.ListContactsStepConfig[keap.ListContactsInput]{
			StepType: listContactsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "List a bounded page of CRM contacts."},
			Connection:  flow.connection, MapToOperationInput: MapToListContactsInput,
			Listed: sdkgo.GoTo(completeDirectory{}),
		})),
		dex.DefineStep(completeDirectory{}),
	}
}

func (flow *DirectoryFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-contacts-page value-type:json editable:false description:"Contact page"
// GetDexSummary shows the contact page in Dex Web.
func (*DirectoryFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contacts-page": page}}, nil
}

// dex:field attribute-key:keap-contacts-page value-type:json editable:false description:"Contact page"
// GetDexDisplay shows the contact page in Dex Web.
func (*DirectoryFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-contacts-page": page}}, nil
}

func readContactsPage(ctx dex.Context) (keap.ContactsPage, error) {
	page, err := contactsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.ContactsPage{}, nil
	}
	return page, err
}

// MapToListContactsInput passes the typed page request to Keap.
func MapToListContactsInput(input keap.ListContactsInput) keap.ListContactsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested contact page to the provider Step."
type startDirectory struct{ dex.StepDefaults }

func (startDirectory) GetStepType() string { return "StartContactDirectory" }
func (startDirectory) WaitFor(dex.Context, keap.ListContactsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startDirectory) Execute(_ dex.Context, input keap.ListContactsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.ListContactsInput](listContactsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed contact page."
type completeDirectory struct {
	dex.StepDefaultsNoWaitFor[keap.ListContactsResult]
}

func (completeDirectory) GetStepType() string { return "CompleteContactDirectory" }
func (completeDirectory) Execute(ctx dex.Context, result keap.ListContactsResult) (*dex.StepDecision, error) {
	if err := contactsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*DirectoryFlow)(nil)
