// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	tagmanager "github.com/superdurable/dex-connectors-library/connectors/google/tag-manager/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const accountInspectionFlowType = "GoogleTagManagerAccountInspection"
const getAccountStepType = "CallGetAccount"

var inspectedAccountAttribute = dex.DefineAttribute[tagmanager.TagManagerResource]("inspected-tag-manager-account")

// AccountInspectionFlow reads one account using the operation-specific GetAccount Step.
type AccountInspectionFlow struct {
	dex.FlowDefaults
	connection tagmanager.Connection
}

// NewAccountInspectionFlow binds the same static connection as the accounts example.
func NewAccountInspectionFlow(connection tagmanager.Connection) *AccountInspectionFlow {
	return &AccountInspectionFlow{connection: connection}
}

// GetFlowType preserves the account inspection example's Flow identity.
func (*AccountInspectionFlow) GetFlowType() string { return accountInspectionFlowType }

// GetPersistenceSchema registers the inspected account Attribute.
func (*AccountInspectionFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedAccountAttribute}}
}

// GetSteps binds the account lookup and completion Steps.
func (flow *AccountInspectionFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAccountInspection{}),
		dex.DefineStep(tagmanager.NewGetAccountStep(tagmanager.GetAccountStepConfig[tagmanager.ResourcePathInput]{
			StepType: getAccountStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Tag Manager", Explanation: "Read a selected account's identity."},
			Connection:          flow.connection,
			MapToOperationInput: mapAccountPath,
			Found:               sdkgo.GoTo(completeAccountInspection{}),
		})),
		dex.DefineStep(completeAccountInspection{}),
	}
}

// GetRPCs provides the Dex Web account summary and display views.
func (flow *AccountInspectionFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:inspected-tag-manager-account value-type:json editable:false description:"Account identity"
// GetDexSummary returns the inspected account identity.
func (*AccountInspectionFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readInspectedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"inspected-tag-manager-account": account}}, nil
}

// dex:field attribute-key:inspected-tag-manager-account value-type:json editable:false description:"Account identity"
// GetDexDisplay returns the inspected account identity.
func (*AccountInspectionFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readInspectedAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"inspected-tag-manager-account": account}}, nil
}

func readInspectedAccount(ctx dex.Context) (tagmanager.TagManagerResource, error) {
	account, err := inspectedAccountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tagmanager.TagManagerResource{}, nil
	}
	return account, err
}

func mapAccountPath(input tagmanager.ResourcePathInput) tagmanager.ResourcePathInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected account path to the provider Step."
type startAccountInspection struct{ dex.StepDefaults }

func (startAccountInspection) GetStepType() string { return "StartAccountInspection" }
func (startAccountInspection) WaitFor(dex.Context, tagmanager.ResourcePathInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAccountInspection) Execute(_ dex.Context, input tagmanager.ResourcePathInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tagmanager.ResourcePathInput](getAccountStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the selected account identity."
type completeAccountInspection struct {
	dex.StepDefaultsNoWaitFor[tagmanager.GetAccountResult]
}

func (completeAccountInspection) GetStepType() string { return "CompleteAccountInspection" }
func (completeAccountInspection) Execute(ctx dex.Context, result tagmanager.GetAccountResult) (*dex.StepDecision, error) {
	if err := inspectedAccountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AccountInspectionFlow)(nil)
