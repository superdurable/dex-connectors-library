// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package policycatalog runs Enlvon searchPolicies as one Dex Flow.
package policycatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/enlvon/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "EnlvonPolicyCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "enlvon"
	operationStepType = "CallSearchPolicies"
)

var policyCatalogOutcomeAttribute = dex.DefineAttribute[enlvon.SearchPoliciesOutput]("enlvon-policy-directory-outcome")

// Flow runs SearchPolicies once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection enlvon.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection enlvon.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{policyCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(enlvon.NewSearchPoliciesStep(enlvon.SearchPoliciesStepConfig[enlvon.SearchPoliciesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Enlvon", Explanation: "Search electricity policies and retain titles, dates, sources, and original links."},
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

// dex:field attribute-key:enlvon-policy-directory-outcome value-type:json editable:false description:"Policy catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPolicyCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"enlvon-policy-directory-outcome": outcome}}, nil
}

// dex:field attribute-key:enlvon-policy-directory-outcome value-type:json editable:false description:"Policy catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPolicyCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"enlvon-policy-directory-outcome": outcome}}, nil
}

func readPolicyCatalogOutcome(ctx dex.Context) (enlvon.SearchPoliciesOutput, error) {
	outcome, err := policyCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return enlvon.SearchPoliciesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input enlvon.SearchPoliciesInput) enlvon.SearchPoliciesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartSearchPolicies" }
func (startRequest) WaitFor(dex.Context, enlvon.SearchPoliciesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input enlvon.SearchPoliciesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[enlvon.SearchPoliciesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[enlvon.SearchPoliciesResult]
}

func (completeResult) GetStepType() string { return "CompleteSearchPolicies" }
func (completeResult) Execute(ctx dex.Context, result enlvon.SearchPoliciesResult) (*dex.StepDecision, error) {
	if err := policyCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
