// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package domainassets runs FOFA searchDomainAssets as one Dex Flow.
package domainassets

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/fofa/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FOFADomainAssetSearch"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "fofa"
	operationStepType = "CallSearchDomainAssets"
)

var domainAssetsOutcomeAttribute = dex.DefineAttribute[fofa.SearchDomainAssetsOutput]("fofa-search-domain-assets-outcome")

// Flow runs SearchDomainAssets once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection fofa.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection fofa.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{domainAssetsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(fofa.NewSearchDomainAssetsStep(fofa.SearchDomainAssetsStepConfig[fofa.SearchDomainAssetsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "FOFA", Explanation: "Search indexed public hosts for the supplied domain."},
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

// dex:field attribute-key:fofa-search-domain-assets-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSearchDomainAssetsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fofa-search-domain-assets-outcome": outcome}}, nil
}

// dex:field attribute-key:fofa-search-domain-assets-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readSearchDomainAssetsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"fofa-search-domain-assets-outcome": outcome}}, nil
}

func readSearchDomainAssetsOutcome(ctx dex.Context) (fofa.SearchDomainAssetsOutput, error) {
	outcome, err := domainAssetsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return fofa.SearchDomainAssetsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input fofa.SearchDomainAssetsInput) fofa.SearchDomainAssetsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchDomainAssets" }
func (startRequest) WaitFor(dex.Context, fofa.SearchDomainAssetsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input fofa.SearchDomainAssetsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[fofa.SearchDomainAssetsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[fofa.SearchDomainAssetsResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchDomainAssets" }
func (completeResult) Execute(ctx dex.Context, result fofa.SearchDomainAssetsResult) (*dex.StepDecision, error) {
	if err := domainAssetsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
