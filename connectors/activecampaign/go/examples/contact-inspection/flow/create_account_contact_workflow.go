// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAccountContactStepType = "CallCreateAccountContact"

var createdAccountContactAttribute = dex.DefineAttribute[activecampaign.AccountContactAssociation]("activecampaign-created-account-contact")

// CreateAccountContactFlow demonstrates a repeat-safe account create through a Dex Step.
type CreateAccountContactFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateAccountContactFlow binds the same static connection as the inspection Flow.
func NewCreateAccountContactFlow(connection activecampaign.Connection) *CreateAccountContactFlow {
	return &CreateAccountContactFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateAccountContactFlow) GetFlowType() string { return "ActiveCampaignCreateAccountContact" }

// GetPersistenceSchema registers the created tag displayed in Dex Web.
func (*CreateAccountContactFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdAccountContactAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateAccountContactFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateAccountContact{}),
		dex.DefineStep(activecampaign.NewCreateAccountContactStep(activecampaign.CreateAccountContactStepConfig[activecampaign.CreateAccountContactInput]{
			StepType: createAccountContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Link one contact to one CRM account."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateAccountContactInput,
			Created: sdkgo.GoTo(completeCreateAccountContact{}),
		})),
		dex.DefineStep(completeCreateAccountContact{}),
	}
}

// GetRPCs exposes the stored link in Dex Web.
func (flow *CreateAccountContactFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-account-contact value-type:json editable:false description:"Created account-contact link"
// GetDexSummary shows the created link in Dex Web.
func (*CreateAccountContactFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccountContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-account-contact": account}}, nil
}

// dex:field attribute-key:activecampaign-created-account-contact value-type:json editable:false description:"Created account-contact link"
// GetDexDisplay shows the created link in Dex Web.
func (*CreateAccountContactFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccountContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-account-contact": account}}, nil
}

func readCreatedAccountContact(ctx dex.Context) (activecampaign.AccountContactAssociation, error) {
	account, err := createdAccountContactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.AccountContactAssociation{}, nil
	}
	return account, err
}

// MapToCreateAccountContactInput preserves the typed start input for the provider call.
func MapToCreateAccountContactInput(input activecampaign.CreateAccountContactInput) activecampaign.CreateAccountContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the link fields to the create Step."
type startCreateAccountContact struct{ dex.StepDefaults }

func (startCreateAccountContact) GetStepType() string { return "StartCreateAccountContact" }
func (startCreateAccountContact) WaitFor(dex.Context, activecampaign.CreateAccountContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateAccountContact) Execute(_ dex.Context, input activecampaign.CreateAccountContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateAccountContactInput](createAccountContactStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created account-contact link metadata."
type completeCreateAccountContact struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateAccountContactResult]
}

func (completeCreateAccountContact) GetStepType() string { return "CompleteCreateAccountContact" }
func (completeCreateAccountContact) Execute(ctx dex.Context, result activecampaign.CreateAccountContactResult) (*dex.StepDecision, error) {
	if err := createdAccountContactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAccountContactFlow)(nil)
