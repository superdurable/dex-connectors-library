// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountinfo runs ApiZero getAccountInfo as one Dex Flow.
package accountinfo

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/apizero/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "ApiZeroAccountInfo"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "apizero"
	operationStepType = "CallGetAccountInfo"
)

var accountInfoOutcomeAttribute = dex.DefineAttribute[apizero.GetAccountInfoOutput]("apizero-get-account-info-outcome")

// Flow runs GetAccountInfo once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection apizero.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection apizero.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountInfoOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(apizero.NewGetAccountInfoStep(apizero.GetAccountInfoStepConfig[apizero.GetAccountInfoInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "ApiZero", Explanation: "Read the connected Key's account balance or prepaid call quota."},
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

// dex:field attribute-key:apizero-get-account-info-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetAccountInfoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apizero-get-account-info-outcome": outcome}}, nil
}

// dex:field attribute-key:apizero-get-account-info-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetAccountInfoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apizero-get-account-info-outcome": outcome}}, nil
}

func readGetAccountInfoOutcome(ctx dex.Context) (apizero.GetAccountInfoOutput, error) {
	outcome, err := accountInfoOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return apizero.GetAccountInfoOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input apizero.GetAccountInfoInput) apizero.GetAccountInfoInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccountInfo" }
func (startRequest) WaitFor(dex.Context, apizero.GetAccountInfoInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input apizero.GetAccountInfoInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[apizero.GetAccountInfoInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[apizero.GetAccountInfoResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccountInfo" }
func (completeResult) Execute(ctx dex.Context, result apizero.GetAccountInfoResult) (*dex.StepDecision, error) {
	if err := accountInfoOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
