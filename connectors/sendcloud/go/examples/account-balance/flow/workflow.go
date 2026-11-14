// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountbalance runs SendCloud getAccountBalance as one Dex Flow.
package accountbalance

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/sendcloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SendCloudAccountBalance"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "sendcloud"
	operationStepType = "CallGetAccountBalance"
)

var accountBalanceOutcomeAttribute = dex.DefineAttribute[sendcloud.GetAccountBalanceOutput]("sendcloud-account-balance-outcome")

// Flow runs GetAccountBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection sendcloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection sendcloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountBalanceOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(sendcloud.NewGetAccountBalanceStep(sendcloud.GetAccountBalanceStepConfig[sendcloud.GetAccountBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SendCloud", Explanation: "Read the SendCloud account balance and daily quota."},
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

// dex:field attribute-key:sendcloud-account-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendcloud-account-balance-outcome": outcome}}, nil
}

// dex:field attribute-key:sendcloud-account-balance-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccountBalanceOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sendcloud-account-balance-outcome": outcome}}, nil
}

func readAccountBalanceOutcome(ctx dex.Context) (sendcloud.GetAccountBalanceOutput, error) {
	outcome, err := accountBalanceOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sendcloud.GetAccountBalanceOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input sendcloud.GetAccountBalanceInput) sendcloud.GetAccountBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountBalance" }
func (startRequest) WaitFor(dex.Context, sendcloud.GetAccountBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input sendcloud.GetAccountBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[sendcloud.GetAccountBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[sendcloud.GetAccountBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountBalance" }
func (completeResult) Execute(ctx dex.Context, result sendcloud.GetAccountBalanceResult) (*dex.StepDecision, error) {
	if err := accountBalanceOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
