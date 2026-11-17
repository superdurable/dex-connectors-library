// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadstatusdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listCustomerSourcesStepType = "ListCopperCustomerSources"

var customerSourcesAttribute = dex.DefineAttribute[copper.CustomerSourcesList]("copper-customer-sources")

// CustomerSourcesFlow reads Copper's configured customer sources.
type CustomerSourcesFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewCustomerSourcesFlow binds the static Copper connection.
func NewCustomerSourcesFlow(connection copper.Connection) *CustomerSourcesFlow {
	return &CustomerSourcesFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*CustomerSourcesFlow) GetFlowType() string { return "CopperCustomerSourcesDiscovery" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*CustomerSourcesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerSourcesAttribute}}
}

// GetSteps wires the operation-specific customerSources Query.
func (flow *CustomerSourcesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerSources{}),
		dex.DefineStep(copper.NewListCustomerSourcesStep(copper.ListCustomerSourcesStepConfig[copper.ListCustomerSourcesInput]{
			StepType: listCustomerSourcesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper customerSources."},
			Connection:  flow.connection, MapToOperationInput: mapCustomerSourcesInput,
			Listed: sdkgo.GoTo(completeCustomerSources{}),
		})),
		dex.DefineStep(completeCustomerSources{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *CustomerSourcesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-customer-sources value-type:json editable:false description:"Customer source IDs and labels"
// GetDexSummary shows the returned page in Dex Web.
func (*CustomerSourcesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerSourcesList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-customer-sources": page}}, nil
}

// dex:field attribute-key:copper-customer-sources value-type:json editable:false description:"Customer source IDs and labels"
// GetDexDisplay shows the returned page in Dex Web.
func (*CustomerSourcesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerSourcesList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-customer-sources": page}}, nil
}

func readCustomerSourcesList(ctx dex.Context) (copper.CustomerSourcesList, error) {
	page, err := customerSourcesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.CustomerSourcesList{}, nil
	}
	return page, err
}

func mapCustomerSourcesInput(input copper.ListCustomerSourcesInput) copper.ListCustomerSourcesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested customerSources page and filters to Copper."
type startCustomerSources struct{ dex.StepDefaults }

func (startCustomerSources) WaitFor(dex.Context, copper.ListCustomerSourcesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerSources) Execute(_ dex.Context, input copper.ListCustomerSourcesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListCustomerSourcesInput](listCustomerSourcesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the customer source list and complete the Flow."
type completeCustomerSources struct {
	dex.StepDefaultsNoWaitFor[copper.ListCustomerSourcesResult]
}

func (completeCustomerSources) Execute(ctx dex.Context, result copper.ListCustomerSourcesResult) (*dex.StepDecision, error) {
	if err := customerSourcesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerSourcesFlow)(nil)
