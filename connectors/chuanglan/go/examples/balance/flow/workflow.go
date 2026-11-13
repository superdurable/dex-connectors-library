// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package balanceflow runs Chuanglan 253 getBalance as one Dex Flow.
package balanceflow

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/chuanglan/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ChuanglanBalance"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "chuanglan"
	operationStepType = "CallGetBalance"
)

var balanceOutcomeAttribute = dex.DefineAttribute[chuanglan.GetBalanceOutput]("chuanglan-balance-outcome")

// Flow runs GetBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection chuanglan.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection chuanglan.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{balanceOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(chuanglan.NewGetBalanceStep(chuanglan.GetBalanceStepConfig[chuanglan.GetBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Chuanglan 253", Explanation: "Read the status flags for the requested Balance."},
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

// dex:field attribute-key:chuanglan-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chuanglan-balance-outcome": outcome}}, nil
}

// dex:field attribute-key:chuanglan-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"chuanglan-balance-outcome": outcome}}, nil
}

func readBalanceOutcome(ctx dex.Context) (chuanglan.GetBalanceOutput, error) {
	outcome, err := balanceOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return chuanglan.GetBalanceOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input chuanglan.GetBalanceInput) chuanglan.GetBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetBalance" }
func (startRequest) WaitFor(dex.Context, chuanglan.GetBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input chuanglan.GetBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[chuanglan.GetBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[chuanglan.GetBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetBalance" }
func (completeResult) Execute(ctx dex.Context, result chuanglan.GetBalanceResult) (*dex.StepDecision, error) {
	if err := balanceOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
