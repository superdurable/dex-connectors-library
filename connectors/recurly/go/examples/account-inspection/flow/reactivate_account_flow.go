// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const reactivateAccountFlowType = "RecurlyReactivateAccount"
const reactivateAccountStepType = "CallReactivateAccount"

var reactivatedAccountAttribute = dex.DefineAttribute[recurly.AccountLifecycle]("recurly-reactivated-account")

// ReactivateAccountFlow reactivates one inactive account.
type ReactivateAccountFlow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewReactivateAccountFlow binds the static Recurly connection.
func NewReactivateAccountFlow(connection recurly.Connection) *ReactivateAccountFlow {
	return &ReactivateAccountFlow{connection: connection}
}

func (*ReactivateAccountFlow) GetFlowType() string { return reactivateAccountFlowType }

func (*ReactivateAccountFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{reactivatedAccountAttribute}}
}

func (flow *ReactivateAccountFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startReactivateAccount{}),
		dex.DefineStep(recurly.NewReactivateAccountStep(recurly.ReactivateAccountStepConfig[recurly.ReactivateAccountInput]{
			StepType: reactivateAccountStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Reactivate one inactive account."},
			Connection:          flow.connection,
			MapToOperationInput: mapReactivateAccountInput,
			Reactivated:         sdkgo.GoTo(completeReactivateAccount{}),
		})),
		dex.DefineStep(completeReactivateAccount{}),
	}
}

func (flow *ReactivateAccountFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:recurly-reactivated-account value-type:json editable:false description:"Reactivated account"
// GetDexSummary returns the safe account summary for Dex Web.
func (*ReactivateAccountFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readReactivatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-reactivated-account": value}}, nil
}

// dex:field attribute-key:recurly-reactivated-account value-type:json editable:false description:"Reactivated account"
// GetDexDisplay returns the safe account summary for Dex Web.
func (*ReactivateAccountFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readReactivatedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-reactivated-account": value}}, nil
}

func readReactivatedAccount(ctx dex.Context) (recurly.AccountLifecycle, error) {
	value, err := reactivatedAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.AccountLifecycle{}, nil
	}
	return value, err
}

func mapReactivateAccountInput(input recurly.ReactivateAccountInput) recurly.ReactivateAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass amount and currency to the provider Step."
type startReactivateAccount struct{ dex.StepDefaults }

func (startReactivateAccount) GetStepType() string { return "StartReactivateAccount" }
func (startReactivateAccount) WaitFor(dex.Context, recurly.ReactivateAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startReactivateAccount) Execute(_ dex.Context, input recurly.ReactivateAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.ReactivateAccountInput](reactivateAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after Recurly confirms the account."
type completeReactivateAccount struct {
	dex.StepDefaultsNoWaitFor[recurly.ReactivateAccountResult]
}

func (completeReactivateAccount) GetStepType() string { return "CompleteReactivateAccount" }
func (completeReactivateAccount) Execute(ctx dex.Context, result recurly.ReactivateAccountResult) (*dex.StepDecision, error) {
	if err := reactivatedAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ReactivateAccountFlow)(nil)
