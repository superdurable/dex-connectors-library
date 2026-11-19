// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/dynamics-365-sales/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAccountStepType = "CallCreateAccount"

var accountAttribute = dex.DefineAttribute[dynamics365sales.Account]("dynamics-created-account")

// AccountCreationFlow creates one account from a supplied name.
type AccountCreationFlow struct {
	dex.FlowDefaults
	connection dynamics365sales.Connection
}

// NewAccountCreationFlow binds the named Dynamics connection.
func NewAccountCreationFlow(connection dynamics365sales.Connection) *AccountCreationFlow {
	return &AccountCreationFlow{connection: connection}
}

// GetPersistenceSchema registers the created account for Dex Web.
func (*AccountCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountAttribute}}
}

// GetSteps registers the request, provider, and completion Steps.
func (flow *AccountCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAccountCreation{}),
		dex.DefineStep(dynamics365sales.NewCreateAccountStep(dynamics365sales.CreateAccountStepConfig[dynamics365sales.CreateAccountInput]{
			StepType: createAccountStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dynamics 365 Sales", Explanation: "Create an account with a repeat-safe ID."},
			Connection:          flow.connection,
			MapToOperationInput: func(input dynamics365sales.CreateAccountInput) dynamics365sales.CreateAccountInput { return input },
			Created:             sdkgo.GoTo(completeAccountCreation{}),
		})),
		dex.DefineStep(completeAccountCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web account views.
func (flow *AccountCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dynamics-created-account value-type:json editable:false description:"Created account"
// GetDexSummary returns the created account identity.
func (*AccountCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamics-created-account": account}}, nil
}

// dex:field attribute-key:dynamics-created-account value-type:json editable:false description:"Created account"
// GetDexDisplay returns the created account for Dex Web.
func (*AccountCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamics-created-account": account}}, nil
}

func readCreatedAccount(ctx dex.Context) (dynamics365sales.Account, error) {
	account, err := accountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dynamics365sales.Account{}, nil
	}
	return account, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account name to the provider Step."
type startAccountCreation struct{ dex.StepDefaults }

func (startAccountCreation) WaitFor(dex.Context, dynamics365sales.CreateAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAccountCreation) Execute(_ dex.Context, input dynamics365sales.CreateAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dynamics365sales.CreateAccountInput](createAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created account identity."
type completeAccountCreation struct {
	dex.StepDefaultsNoWaitFor[dynamics365sales.CreateAccountResult]
}

func (completeAccountCreation) Execute(ctx dex.Context, result dynamics365sales.CreateAccountResult) (*dex.StepDecision, error) {
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AccountCreationFlow)(nil)
