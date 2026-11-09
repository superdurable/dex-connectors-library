// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accounts runs Google Tag Manager listAccounts as one Dex Flow.
package accounts

import (
	"errors"

	tagmanager "github.com/superdurable/dex-connectors-library/connectors/google/tag-manager/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleTagManagerAccounts"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-tag-manager"
	operationStepType = "CallListAccounts"
)

var accountsAttribute = dex.DefineAttribute[tagmanager.AccountsPage]("google-tag-manager-accounts")

// Flow runs ListAccounts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tagmanager.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tagmanager.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tagmanager.NewListAccountsStep(tagmanager.ListAccountsStepConfig[tagmanager.ListAccountsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Tag Manager", Explanation: "List accounts available to the connected Tag Manager user."},
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

// dex:field attribute-key:google-tag-manager-accounts value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-tag-manager-accounts": outcome}}, nil
}

// dex:field attribute-key:google-tag-manager-accounts value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccounts(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-tag-manager-accounts": outcome}}, nil
}

func readAccounts(ctx dex.Context) (tagmanager.AccountsPage, error) {
	outcome, err := accountsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tagmanager.AccountsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tagmanager.ListAccountsInput) tagmanager.ListAccountsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListAccounts" }
func (startRequest) WaitFor(dex.Context, tagmanager.ListAccountsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tagmanager.ListAccountsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tagmanager.ListAccountsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tagmanager.ListAccountsResult]
}

func (completeResult) GetStepType() string { return "CompleteListAccounts" }
func (completeResult) Execute(ctx dex.Context, result tagmanager.ListAccountsResult) (*dex.StepDecision, error) {
	if err := accountsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
