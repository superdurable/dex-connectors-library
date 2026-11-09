// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contacts runs Google Contacts listContacts as one Dex Flow.
package contacts

import (
	"errors"

	contacts "github.com/superdurable/dex-connectors-library/connectors/google/contacts/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleContactsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-contacts"
	operationStepType = "CallListContacts"
)

var contactsAttribute = dex.DefineAttribute[contacts.ContactsPage]("google-contacts-page")

// Flow runs ListContacts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection contacts.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection contacts.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(contacts.NewListContactsStep(contacts.ListContactsStepConfig[contacts.ListContactsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Contacts", Explanation: "List contacts available to the connected Google user."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-contacts-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContacts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-contacts-page": outcome}}, nil
}

// dex:field attribute-key:google-contacts-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContacts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-contacts-page": outcome}}, nil
}

func readContacts(ctx dex.Context) (contacts.ContactsPage, error) {
	outcome, err := contactsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return contacts.ContactsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input contacts.ListContactsInput) contacts.ListContactsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListContacts" }
func (startRequest) WaitFor(dex.Context, contacts.ListContactsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input contacts.ListContactsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[contacts.ListContactsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[contacts.ListContactsResult]
}

func (completeResult) GetStepType() string { return "CompleteListContacts" }
func (completeResult) Execute(ctx dex.Context, result contacts.ListContactsResult) (*dex.StepDecision, error) {
	if err := contactsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
