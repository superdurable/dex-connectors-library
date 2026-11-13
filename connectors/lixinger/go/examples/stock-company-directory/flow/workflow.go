// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package companycatalog runs Lixinger listStockCompanies as one Dex Flow.
package companycatalog

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lixinger/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "LixingerCompanyCatalog"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "lixinger"
	operationStepType = "CallListStockCompanies"
)

var companyCatalogOutcomeAttribute = dex.DefineAttribute[lixinger.ListStockCompaniesOutput]("lixinger-company-catalog-outcome")

// Flow runs ListStockCompanies once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection lixinger.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection lixinger.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyCatalogOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(lixinger.NewListStockCompaniesStep(lixinger.ListStockCompaniesStepConfig[lixinger.ListStockCompaniesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lixinger", Explanation: "Read basic listing metadata for the supplied A-share stock codes."},
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

// dex:field attribute-key:lixinger-company-catalog-outcome value-type:json editable:false description:"Company catalog"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCompanyCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lixinger-company-catalog-outcome": outcome}}, nil
}

// dex:field attribute-key:lixinger-company-catalog-outcome value-type:json editable:false description:"Company catalog"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCompanyCatalogOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lixinger-company-catalog-outcome": outcome}}, nil
}

func readCompanyCatalogOutcome(ctx dex.Context) (lixinger.ListStockCompaniesOutput, error) {
	outcome, err := companyCatalogOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lixinger.ListStockCompaniesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input lixinger.ListStockCompaniesInput) lixinger.ListStockCompaniesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListStockCompanies" }
func (startRequest) WaitFor(dex.Context, lixinger.ListStockCompaniesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input lixinger.ListStockCompaniesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lixinger.ListStockCompaniesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[lixinger.ListStockCompaniesResult]
}

func (completeResult) GetStepType() string { return "CompleteListStockCompanies" }
func (completeResult) Execute(ctx dex.Context, result lixinger.ListStockCompaniesResult) (*dex.StepDecision, error) {
	if err := companyCatalogOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
