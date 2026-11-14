// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package balanceread runs Yunpian getAccountBalance as one Dex Flow.
package balanceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/yunpian/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "YunpianBalanceRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "yunpian"
	operationStepType = "CallGetAccountBalance"
)

var balanceAttribute = dex.DefineAttribute[yunpian.AccountBalance]("yunpian-balance-read")

// Flow runs GetAccountBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection yunpian.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection yunpian.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{balanceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(yunpian.NewGetAccountBalanceStep(yunpian.GetAccountBalanceStepConfig[yunpian.GetAccountBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Yunpian", Explanation: "Read the Yunpian account balance."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Read:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:yunpian-balance-read value-type:json editable:false description:"Account balance"
// GetDexSummary shows the last account balance in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalance(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"yunpian-balance-read": outcome}}, nil
}

// dex:field attribute-key:yunpian-balance-read value-type:json editable:false description:"Account balance"
// GetDexDisplay shows the last account balance in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalance(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"yunpian-balance-read": outcome}}, nil
}

func readBalance(ctx dex.Context) (yunpian.AccountBalance, error) {
	outcome, err := balanceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return yunpian.AccountBalance{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input yunpian.GetAccountBalanceInput) yunpian.GetAccountBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountBalance" }
func (startRequest) WaitFor(dex.Context, yunpian.GetAccountBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input yunpian.GetAccountBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[yunpian.GetAccountBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[yunpian.GetAccountBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountBalance" }
func (completeResult) Execute(ctx dex.Context, result yunpian.GetAccountBalanceResult) (*dex.StepDecision, error) {
	if err := balanceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
