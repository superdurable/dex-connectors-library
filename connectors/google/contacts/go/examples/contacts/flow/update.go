// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"errors"

	contacts "github.com/superdurable/dex-connectors-library/connectors/google/contacts/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// UpdateFlowType is the stable example contact update identity.
	UpdateFlowType = "GoogleContactsUpdate"
	updateStepType = "CallUpdateContact"
)

var updatedContactAttribute = dex.DefineAttribute[contacts.Contact]("google-contacts-updated")

// UpdateFlow updates one contact using an etag from getContactRevision.
type UpdateFlow struct {
	dex.FlowDefaults
	connection contacts.Connection
}

// NewUpdateFlow binds the provider connection before registration.
func NewUpdateFlow(connection contacts.Connection) *UpdateFlow {
	return &UpdateFlow{connection: connection}
}

func (*UpdateFlow) GetFlowType() string { return UpdateFlowType }

func (*UpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedContactAttribute}}
}

func (flow *UpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContactUpdate{}),
		dex.DefineStep(contacts.NewUpdateContactStep(contacts.UpdateContactStepConfig[contacts.UpdateContactInput]{
			StepType: updateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Contacts", Explanation: "Update a contact at its known source revision."},
			Connection:          flow.connection,
			MapToOperationInput: mapUpdateInput,
			Updated:             sdkgo.GoTo(completeContactUpdate{}),
		})),
		dex.DefineStep(completeContactUpdate{}),
	}
}

// GetRPCs exposes read-only contact summary and display views.
func (flow *UpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-contacts-updated value-type:json editable:false description:"Updated contact"
// GetDexSummary shows the updated contact in Dex Web.
func (*UpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-contacts-updated": contact}}, nil
}

// dex:field attribute-key:google-contacts-updated value-type:json editable:false description:"Updated contact"
// GetDexDisplay shows the updated contact in Dex Web.
func (*UpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-contacts-updated": contact}}, nil
}

func readUpdatedContact(ctx dex.Context) (contacts.Contact, error) {
	contact, err := updatedContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return contacts.Contact{}, nil
	}
	return contact, err
}

func mapUpdateInput(input contacts.UpdateContactInput) contacts.UpdateContactInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the contact revision and replacement fields to Google."
type startContactUpdate struct{ dex.StepDefaults }

func (startContactUpdate) GetStepType() string { return "StartUpdateContact" }
func (startContactUpdate) WaitFor(dex.Context, contacts.UpdateContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContactUpdate) Execute(_ dex.Context, input contacts.UpdateContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[contacts.UpdateContactInput](updateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store and return the updated contact."
type completeContactUpdate struct {
	dex.StepDefaultsNoWaitFor[contacts.UpdateContactResult]
}

func (completeContactUpdate) GetStepType() string { return "CompleteUpdateContact" }
func (completeContactUpdate) Execute(ctx dex.Context, result contacts.UpdateContactResult) (*dex.StepDecision, error) {
	if err := updatedContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateFlow)(nil)
