// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package domaincatalog runs West listDomains as one Dex Flow.
package domaincatalog

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/west/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WestDomainCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "west"
	operationStepType = "CallListDomains"
)

var domainCatalogOutcomeAttribute = dex.DefineAttribute[west.ListDomainsOutput]("west-domain-catalog-outcome")

// Flow runs ListDomains once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection west.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection west.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{domainCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(west.NewListDomainsStep(west.ListDomainsStepConfig[west.ListDomainsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "West", Explanation: "Read one page of domain names and expiration dates from the connected West.cn reseller account."},
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

// dex:field attribute-key:west-domain-catalog-outcome value-type:json editable:false description:"Domain catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDomainCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"west-domain-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:west-domain-catalog-outcome value-type:json editable:false description:"Domain catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDomainCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"west-domain-catalog-outcome": outcome}}, nil
}

func readDomainCatalogOutcome(ctx dex.Context) (west.ListDomainsOutput, error) {
	outcome, err := domainCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return west.ListDomainsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input west.ListDomainsInput) west.ListDomainsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDomains" }
func (startRequest) WaitFor(dex.Context, west.ListDomainsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input west.ListDomainsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[west.ListDomainsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[west.ListDomainsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDomains" }
func (completeResult) Execute(ctx dex.Context, result west.ListDomainsResult) (*dex.StepDecision, error) {
	if err := domainCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
