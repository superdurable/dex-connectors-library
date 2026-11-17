// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/creatio/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAccountStepType = "CreateCreatioAccount"

var createdAccountAttribute = dex.DefineAttribute[creatio.AccountSummary]("creatio-created-account")

// AccountCreationFlow creates one Creatio account with bounded sends.
type AccountCreationFlow struct {
	dex.FlowDefaults
	connection creatio.Connection
}

// NewAccountCreationFlow binds the static Creatio connection.
func NewAccountCreationFlow(connection creatio.Connection) *AccountCreationFlow {
	return &AccountCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*AccountCreationFlow) GetFlowType() string { return "CreatioAccountCreation" }

// GetPersistenceSchema retains the created account for Dex Web.
func (*AccountCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdAccountAttribute}}
}

// GetSteps wires the operation-specific account Mutation.
func (flow *AccountCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAccountCreation{}),
		dex.DefineStep(creatio.NewCreateAccountStep(creatio.CreateAccountStepConfig[creatio.CreateAccountInput]{
			StepType: createAccountStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Creatio", Explanation: "Create a Creatio account."},
			Connection:  flow.connection, MapToOperationInput: mapAccountCreationInput,
			Created: sdkgo.GoTo(completeAccountCreation{}),
		})),
		dex.DefineStep(completeAccountCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *AccountCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:creatio-created-account value-type:json editable:false description:"Created account"
// GetDexSummary shows the created account in Dex Web.
func (*AccountCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAccountMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-account": page}}, nil
}

// dex:field attribute-key:creatio-created-account value-type:json editable:false description:"Created account"
// GetDexDisplay shows the created account in Dex Web.
func (*AccountCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readAccountMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"creatio-created-account": page}}, nil
}

func readAccountMetadata(ctx dex.Context) (creatio.AccountSummary, error) {
	page, err := createdAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return creatio.AccountSummary{}, nil
	}
	return page, err
}

func mapAccountCreationInput(input creatio.CreateAccountInput) creatio.CreateAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested account name to Creatio."
type startAccountCreation struct{ dex.StepDefaults }

func (startAccountCreation) WaitFor(dex.Context, creatio.CreateAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAccountCreation) Execute(_ dex.Context, input creatio.CreateAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[creatio.CreateAccountInput](createAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created account and complete the Flow."
type completeAccountCreation struct {
	dex.StepDefaultsNoWaitFor[creatio.CreateAccountResult]
}

func (completeAccountCreation) Execute(ctx dex.Context, result creatio.CreateAccountResult) (*dex.StepDecision, error) {
	if err := createdAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AccountCreationFlow)(nil)
