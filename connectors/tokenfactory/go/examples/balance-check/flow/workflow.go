// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package balancecheck runs TokenFactory getBalance as one Dex Flow.
package balancecheck

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/tokenfactory/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TokenFactoryBalance"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tokenfactory"
	operationStepType = "CallGetBalance"
)

var balanceListOutcomeAttribute = dex.DefineAttribute[tokenfactory.Balance]("tokenfactory-balance-check-outcome")

// Flow runs GetBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tokenfactory.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tokenfactory.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{balanceListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tokenfactory.NewGetBalanceStep(tokenfactory.GetBalanceStepConfig[tokenfactory.GetBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TokenFactory", Explanation: "Read the current TokenFactory account balance in yuan."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Retrieved:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tokenfactory-balance-check-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tokenfactory-balance-check-outcome": outcome}}, nil
}

// dex:field attribute-key:tokenfactory-balance-check-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tokenfactory-balance-check-outcome": outcome}}, nil
}

func readBalanceOutcome(ctx dex.Context) (tokenfactory.Balance, error) {
	outcome, err := balanceListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tokenfactory.Balance{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tokenfactory.GetBalanceInput) tokenfactory.GetBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetBalance" }
func (startRequest) WaitFor(dex.Context, tokenfactory.GetBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tokenfactory.GetBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tokenfactory.GetBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tokenfactory.GetBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetBalance" }
func (completeResult) Execute(ctx dex.Context, result tokenfactory.GetBalanceResult) (*dex.StepDecision, error) {
	if err := balanceListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
