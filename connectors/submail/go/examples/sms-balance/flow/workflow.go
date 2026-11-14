// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package smsbalance runs SUBMAIL getSMSBalance as one Dex Flow.
package smsbalance

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/submail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SUBMAILSMSBalance"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "submail"
	operationStepType = "CallGetSMSBalance"
)

var balanceAttribute = dex.DefineAttribute[submail.SMSBalance]("submail-sms-balance")

// Flow runs GetSMSBalance once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection submail.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection submail.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable SMS balance Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the SMS balance attribute.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{balanceAttribute}}
}

// GetSteps wires the request, provider Query, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(submail.NewGetSMSBalanceStep(submail.GetSMSBalanceStepConfig[submail.GetSMSBalanceInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "SUBMAIL", Explanation: "Read SUBMAIL SMS credits."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:submail-sms-balance value-type:json editable:false description:"SUBMAIL SMS balance"
// GetDexSummary shows the last SUBMAIL SMS balance in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalance(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"submail-sms-balance": outcome}}, nil
}

// dex:field attribute-key:submail-sms-balance value-type:json editable:false description:"SUBMAIL SMS balance"
// GetDexDisplay shows the last SUBMAIL SMS balance in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readBalance(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"submail-sms-balance": outcome}}, nil
}

func readBalance(ctx dex.Context) (submail.SMSBalance, error) {
	outcome, err := balanceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return submail.SMSBalance{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input submail.GetSMSBalanceInput) submail.GetSMSBalanceInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetSMSBalance" }
func (startRequest) WaitFor(dex.Context, submail.GetSMSBalanceInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input submail.GetSMSBalanceInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[submail.GetSMSBalanceInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[submail.GetSMSBalanceResult]
}

func (completeResult) GetStepType() string { return "CompleteGetSMSBalance" }
func (completeResult) Execute(ctx dex.Context, result submail.GetSMSBalanceResult) (*dex.StepDecision, error) {
	if err := balanceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
