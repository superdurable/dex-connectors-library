// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/constantcontact/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createContactStepType = "CreateConstantContactContact"

var createdContactAttribute = dex.DefineAttribute[constantcontact.ContactMetadata]("constantcontact-contact")

// CreateContactFlow creates an email contact and retains its public result.
type CreateContactFlow struct {
	dex.FlowDefaults
	connection constantcontact.Connection
}

// NewCreateContactFlow binds the static Constant Contact connection.
func NewCreateContactFlow(connection constantcontact.Connection) *CreateContactFlow {
	return &CreateContactFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*CreateContactFlow) GetFlowType() string { return "ConstantContactContactCreation" }

// GetPersistenceSchema retains the created contact without credentials.
func (*CreateContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdContactAttribute}}
}

// GetSteps creates the contact only in the provider Step Execute.
func (flow *CreateContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateContact{}),
		dex.DefineStep(constantcontact.NewCreateContactStep(constantcontact.CreateContactStepConfig[constantcontact.CreateContactInput]{
			StepType: createContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Constant Contact", Explanation: "Create one email contact with its recorded permission."},
			Connection:  flow.connection, MapToOperationInput: mapCreateContactInput,
			Created: sdkgo.GoTo(completeCreateContact{}),
		})),
		dex.DefineStep(completeCreateContact{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the created contact.
func (flow *CreateContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:constantcontact-contact value-type:json editable:false description:"Created contact"
// GetDexSummary shows the created contact in Dex Web.
func (*CreateContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readCreatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact": contact}}, nil
}

// dex:field attribute-key:constantcontact-contact value-type:json editable:false description:"Created contact"
// GetDexDisplay shows the created contact in Dex Web.
func (*CreateContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	contact, err := readCreatedContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"constantcontact-contact": contact}}, nil
}

func mapCreateContactInput(input constantcontact.CreateContactInput) constantcontact.CreateContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the consent and contact fields to the creation Step."
type startCreateContact struct{ dex.StepDefaults }

func (startCreateContact) WaitFor(dex.Context, constantcontact.CreateContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateContact) Execute(_ dex.Context, input constantcontact.CreateContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[constantcontact.CreateContactInput](createContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created contact and complete the Flow."
type completeCreateContact struct {
	dex.StepDefaultsNoWaitFor[constantcontact.CreateContactResult]
}

func (completeCreateContact) Execute(ctx dex.Context, result constantcontact.CreateContactResult) (*dex.StepDecision, error) {
	if err := createdContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

func readCreatedContact(ctx dex.Context) (constantcontact.ContactMetadata, error) {
	contact, err := createdContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return constantcontact.ContactMetadata{}, nil
	}
	return contact, err
}

var _ dex.Flow = (*CreateContactFlow)(nil)
