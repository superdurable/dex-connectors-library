// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package organizationcatalog runs Suxin listOrganizations as one Dex Flow.
package organizationcatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/suxin/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SuxinOrganizationCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "suxin"
	operationStepType = "CallListOrganizations"
)

var organizationCatalogOutcomeAttribute = dex.DefineAttribute[suxin.ListOrganizationsOutput]("suxin-organization-directory-outcome")

// Flow runs ListOrganizations once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection suxin.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection suxin.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{organizationCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(suxin.NewListOrganizationsStep(suxin.ListOrganizationsStepConfig[suxin.ListOrganizationsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Suxin", Explanation: "Read accessible organizations with IDs, names, types, and certification states."},
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

// dex:field attribute-key:suxin-organization-directory-outcome value-type:json editable:false description:"Organization catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrganizationCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"suxin-organization-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:suxin-organization-directory-outcome value-type:json editable:false description:"Organization catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOrganizationCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"suxin-organization-directory-outcome": outcome}}, nil
}

func readOrganizationCatalogOutcome(ctx dex.Context) (suxin.ListOrganizationsOutput, error) {
	outcome, err := organizationCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return suxin.ListOrganizationsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input suxin.ListOrganizationsInput) suxin.ListOrganizationsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListOrganizations" }
func (startRequest) WaitFor(dex.Context, suxin.ListOrganizationsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input suxin.ListOrganizationsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[suxin.ListOrganizationsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[suxin.ListOrganizationsResult]
}

func (completeResult) GetStepType() string { return "CompleteListOrganizations" }
func (completeResult) Execute(ctx dex.Context, result suxin.ListOrganizationsResult) (*dex.StepDecision, error) {
	if err := organizationCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
