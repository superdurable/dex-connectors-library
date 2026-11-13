// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountbalance runs Dieyuyun getAccountBalance as one Dex Flow.
package accountbalance

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/dieyuyun/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "DieyuyunAccountBalance"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "dieyuyun"
	operationStepType = "CallGetAccountBalance"
)

var accountBalanceOutcomeAttribute = dex.DefineAttribute[dieyuyun.GetAccountBalanceOutput]("dieyuyun-account-balance-outcome")

// Flow runs GetAccountBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dieyuyun.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dieyuyun.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountBalanceOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dieyuyun.NewGetAccountBalanceStep(dieyuyun.GetAccountBalanceStepConfig[dieyuyun.GetAccountBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dieyuyun", Explanation: "Read the connected Wuliang AI tenant balance in CNY."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dieyuyun-account-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dieyuyun-account-balance-outcome": outcome}}, nil
}

// dex:field attribute-key:dieyuyun-account-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dieyuyun-account-balance-outcome": outcome}}, nil
}

func readAccountBalanceOutcome(ctx dex.Context) (dieyuyun.GetAccountBalanceOutput, error) {
	outcome, err := accountBalanceOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dieyuyun.GetAccountBalanceOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dieyuyun.GetAccountBalanceInput) dieyuyun.GetAccountBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountBalance" }
func (startRequest) WaitFor(dex.Context, dieyuyun.GetAccountBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dieyuyun.GetAccountBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dieyuyun.GetAccountBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dieyuyun.GetAccountBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountBalance" }
func (completeResult) Execute(ctx dex.Context, result dieyuyun.GetAccountBalanceResult) (*dex.StepDecision, error) {
	if err := accountBalanceOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
