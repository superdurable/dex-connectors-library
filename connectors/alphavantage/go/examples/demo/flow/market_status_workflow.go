// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package demo

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/alphavantage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// MarketStatusFlowType identifies the global market status example Flow.
	MarketStatusFlowType = "AlphaVantageMarketStatus"
	marketStatusStepType = "CallGetMarketStatus"
)

var marketStatusAttribute = dex.DefineAttribute[alphavantage.MarketStatusOutput]("alphavantage-market-status-outcome")

// MarketStatusFlow reads global trading venue states for one optional region.
type MarketStatusFlow struct {
	dex.FlowDefaults
	connection alphavantage.Connection
}

// NewMarketStatusFlow binds the same static provider connection as the quote Flow.
func NewMarketStatusFlow(connection alphavantage.Connection) *MarketStatusFlow {
	return &MarketStatusFlow{connection: connection}
}

func (*MarketStatusFlow) GetFlowType() string { return MarketStatusFlowType }
func (*MarketStatusFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{marketStatusAttribute}}
}
func (flow *MarketStatusFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startMarketStatus{}),
		dex.DefineStep(alphavantage.NewGetMarketStatusStep(alphavantage.GetMarketStatusStepConfig[alphavantage.MarketStatusInput]{
			StepType: marketStatusStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Alpha Vantage", Explanation: "Read current trading status for major markets."},
			Connection:  flow.connection, MapToOperationInput: MapToMarketStatusInput,
			Found: sdkgo.GoTo(completeMarketStatus{}),
		})),
		dex.DefineStep(completeMarketStatus{}),
	}
}

// GetRPCs exposes the market status summary and display views.
func (flow *MarketStatusFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:alphavantage-market-status-outcome value-type:json editable:false description:"Trading venue statuses"
// GetDexSummary returns the current market status outcome.
func (*MarketStatusFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMarketStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alphavantage-market-status-outcome": outcome}}, nil
}

// dex:field attribute-key:alphavantage-market-status-outcome value-type:json editable:false description:"Trading venue statuses"
// GetDexDisplay returns the current market status outcome.
func (*MarketStatusFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readMarketStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alphavantage-market-status-outcome": outcome}}, nil
}

func readMarketStatusOutcome(ctx dex.Context) (alphavantage.MarketStatusOutput, error) {
	outcome, err := marketStatusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return alphavantage.MarketStatusOutput{}, nil
	}
	return outcome, err
}

// MapToMarketStatusInput preserves the typed market status request.
func MapToMarketStatusInput(input alphavantage.MarketStatusInput) alphavantage.MarketStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the optional region to the market status Step."
type startMarketStatus struct{ dex.StepDefaults }

func (startMarketStatus) GetStepType() string { return "StartGetMarketStatus" }
func (startMarketStatus) WaitFor(dex.Context, alphavantage.MarketStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startMarketStatus) Execute(_ dex.Context, input alphavantage.MarketStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[alphavantage.MarketStatusInput](marketStatusStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected trading venue statuses."
type completeMarketStatus struct {
	dex.StepDefaultsNoWaitFor[alphavantage.GetMarketStatusResult]
}

func (completeMarketStatus) GetStepType() string { return "CompleteGetMarketStatus" }
func (completeMarketStatus) Execute(ctx dex.Context, result alphavantage.GetMarketStatusResult) (*dex.StepDecision, error) {
	if err := marketStatusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*MarketStatusFlow)(nil)
