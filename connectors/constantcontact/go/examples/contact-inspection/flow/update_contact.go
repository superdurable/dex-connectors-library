// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const updateContactStepType = "UpdateConstantContactContact"

var updatedContactAttribute = dex.DefineAttribute[constantcontact.ContactMetadata]("constantcontact-updated-contact")

// UpdateContactFlow changes selected core fields and retains the result.
type UpdateContactFlow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewUpdateContactFlow binds the static Constant Contact connection.
func NewUpdateContactFlow(connection constantcontact.Connection) *UpdateContactFlow {
	return &UpdateContactFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*UpdateContactFlow) GetFlowType() string { return "ConstantContactContactUpdate" }

// GetPersistenceSchema retains the updated contact without credentials.
func (*UpdateContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedContactAttribute}}
}

// GetSteps updates the provider only in the connector Step Execute.
func (flow *UpdateContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateContact{}),
		dex.DefineStep(constantcontact.NewUpdateContactStep(constantcontact.UpdateContactStepConfig[constantcontact.UpdateContactInput]{
			StepType: updateContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Preserve core fields while updating a contact."},
			Connection:  flow.connection, MapToOperationInput: mapUpdateContactInput,
			Updated: sdkgo.GoTo(completeUpdateContact{}),
		})),
		dex.DefineStep(completeUpdateContact{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the updated contact.
func (flow *UpdateContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:constantcontact-updated-contact value-type:json editable:false description:"Updated contact"
// GetDexSummary shows the updated contact in Dex Web.
func (*UpdateContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-updated-contact": contact}}, nil
}

// dex:field attribute-key:constantcontact-updated-contact value-type:json editable:false description:"Updated contact"
// GetDexDisplay shows the updated contact in Dex Web.
func (*UpdateContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readUpdatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-updated-contact": contact}}, nil
}

func readUpdatedContact(ctx dex.Context) (constantcontact.ContactMetadata, error) {
	contact, err := updatedContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.ContactMetadata{}, nil
	}
	return contact, err
}

func mapUpdateContactInput(input constantcontact.UpdateContactInput) constantcontact.UpdateContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the chosen contact fields to the update Step."
type startUpdateContact struct{ dex.StepDefaults }

func (startUpdateContact) WaitFor(dex.Context, constantcontact.UpdateContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateContact) Execute(_ dex.Context, input constantcontact.UpdateContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.UpdateContactInput](updateContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the updated contact and complete the Flow."
type completeUpdateContact struct {
	dex.StepDefaultsNoWaitFor[constantcontact.UpdateContactResult]
}

func (completeUpdateContact) Execute(ctx dex.Context, result constantcontact.UpdateContactResult) (*dex.StepDecision, error) {
	if err := updatedContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateContactFlow)(nil)
