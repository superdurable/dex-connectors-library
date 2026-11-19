// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createAccountFlowType = "RecurlyCreateAccount"
const createAccountStepType = "CallCreateAccount"

var createdAccountAttribute = dex.DefineAttribute[recurly.AccountIdentity]("recurly-created-account")

// CreateAccountFlow creates one account with a call-specific code.
type CreateAccountFlow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewCreateAccountFlow binds the static Recurly connection.
func NewCreateAccountFlow(connection recurly.Connection) *CreateAccountFlow {
	return &CreateAccountFlow{connection: connection}
}

func (*CreateAccountFlow) GetFlowType() string { return createAccountFlowType }

func (*CreateAccountFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdAccountAttribute}}
}

func (flow *CreateAccountFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateAccount{}),
		dex.DefineStep(recurly.NewCreateAccountStep(recurly.CreateAccountStepConfig[recurly.CreateAccountInput]{
			StepType: createAccountStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Create one account with a call-specific code."},
			Connection:          flow.connection,
			MapToOperationInput: mapCreateAccountInput,
			Created:             sdkgo.GoTo(completeCreateAccount{}),
		})),
		dex.DefineStep(completeCreateAccount{}),
	}
}

func (flow *CreateAccountFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:recurly-created-account value-type:json editable:false description:"Created account"
// GetDexSummary returns the safe account summary for Dex Web.
func (*CreateAccountFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-created-account": value}}, nil
}

// dex:field attribute-key:recurly-created-account value-type:json editable:false description:"Created account"
// GetDexDisplay returns the safe account summary for Dex Web.
func (*CreateAccountFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readCreatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-created-account": value}}, nil
}

func readCreatedAccount(ctx dex.Context) (recurly.AccountIdentity, error) {
	value, err := createdAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.AccountIdentity{}, nil
	}
	return value, err
}

func mapCreateAccountInput(input recurly.CreateAccountInput) recurly.CreateAccountInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass amount and currency to the provider Step."
type startCreateAccount struct{ dex.StepDefaults }

func (startCreateAccount) GetStepType() string { return "StartCreateAccount" }
func (startCreateAccount) WaitFor(dex.Context, recurly.CreateAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateAccount) Execute(_ dex.Context, input recurly.CreateAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.CreateAccountInput](createAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Recurly confirms the account."
type completeCreateAccount struct {
	dex.StepDefaultsNoWaitFor[recurly.CreateAccountResult]
}

func (completeCreateAccount) GetStepType() string { return "CompleteCreateAccount" }
func (completeCreateAccount) Execute(ctx dex.Context, result recurly.CreateAccountResult) (*dex.StepDecision, error) {
	if err := createdAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAccountFlow)(nil)
