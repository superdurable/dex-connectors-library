// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package aweber runs AWeber listAccounts as one Dex Flow.
package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/aweber/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AWeberAccountsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "aweber"
	operationStepType = "CallListAccounts"
)

var accountsAttribute = dex.DefineAttribute[aweber.AccountsPage]("aweber-page")

// Flow runs ListAccounts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection aweber.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection aweber.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(aweber.NewListAccountsStep(aweber.ListAccountsStepConfig[aweber.ListAccountsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "AWeber", Explanation: "List accounts available to the authorized AWeber customer."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:aweber-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"aweber-page": outcome}}, nil
}

// dex:field attribute-key:aweber-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"aweber-page": outcome}}, nil
}

func readAccounts(ctx dex.Context) (aweber.AccountsPage, error) {
	outcome, err := accountsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return aweber.AccountsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input aweber.ListAccountsInput) aweber.ListAccountsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListAccounts" }
func (startRequest) WaitFor(dex.Context, aweber.ListAccountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input aweber.ListAccountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[aweber.ListAccountsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[aweber.ListAccountsResult]
}

func (completeResult) GetStepType() string { return "CompleteListAccounts" }
func (completeResult) Execute(ctx dex.Context, result aweber.ListAccountsResult) (*dex.StepDecision, error) {
	if err := accountsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
