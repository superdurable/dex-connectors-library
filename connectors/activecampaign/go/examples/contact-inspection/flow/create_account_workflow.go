// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/activecampaign/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAccountStepType = "CallCreateAccount"

var createdAccountAttribute = dex.DefineAttribute[activecampaign.AccountMetadata]("activecampaign-created-account")

// CreateAccountFlow demonstrates a repeat-safe account create through a Dex Step.
type CreateAccountFlow struct {
	dex.FlowDefaults
	connection activecampaign.Connection
}

// NewCreateAccountFlow binds the same static connection as the inspection Flow.
func NewCreateAccountFlow(connection activecampaign.Connection) *CreateAccountFlow {
	return &CreateAccountFlow{connection: connection}
}

// GetFlowType is the stable create example identity.
func (*CreateAccountFlow) GetFlowType() string { return "ActiveCampaignCreateAccount" }

// GetPersistenceSchema registers the created tag displayed in Dex Web.
func (*CreateAccountFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdAccountAttribute}}
}

// GetSteps registers the start, provider call, and completion boundary.
func (flow *CreateAccountFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateAccount{}),
		dex.DefineStep(activecampaign.NewCreateAccountStep(activecampaign.CreateAccountStepConfig[activecampaign.CreateAccountInput]{
			StepType: createAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ActiveCampaign", Explanation: "Create one CRM account by name."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateAccountInput,
			Created: sdkgo.GoTo(completeCreateAccount{}),
		})),
		dex.DefineStep(completeCreateAccount{}),
	}
}

// GetRPCs exposes the stored account in Dex Web.
func (flow *CreateAccountFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:activecampaign-created-account value-type:json editable:false description:"Created account"
// GetDexSummary shows the created account in Dex Web.
func (*CreateAccountFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-account": account}}, nil
}

// dex:field attribute-key:activecampaign-created-account value-type:json editable:false description:"Created account"
// GetDexDisplay shows the created account in Dex Web.
func (*CreateAccountFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"activecampaign-created-account": account}}, nil
}

func readCreatedAccount(ctx dex.Context) (activecampaign.AccountMetadata, error) {
	account, err := createdAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return activecampaign.AccountMetadata{}, nil
	}
	return account, err
}

// MapToCreateAccountInput preserves the typed start input for the provider call.
func MapToCreateAccountInput(input activecampaign.CreateAccountInput) activecampaign.CreateAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account fields to the create Step."
type startCreateAccount struct{ dex.StepDefaults }

func (startCreateAccount) GetStepType() string { return "StartCreateAccount" }
func (startCreateAccount) WaitFor(dex.Context, activecampaign.CreateAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateAccount) Execute(_ dex.Context, input activecampaign.CreateAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[activecampaign.CreateAccountInput](createAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created CRM account metadata."
type completeCreateAccount struct {
	dex.StepDefaultsNoWaitFor[activecampaign.CreateAccountResult]
}

func (completeCreateAccount) GetStepType() string { return "CompleteCreateAccount" }
func (completeCreateAccount) Execute(ctx dex.Context, result activecampaign.CreateAccountResult) (*dex.StepDecision, error) {
	if err := createdAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAccountFlow)(nil)
