// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package demo runs Alpha Vantage getGlobalQuote as one Dex Flow.
package demo

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/alphavantage/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AlphaVantageDemo"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "alphavantage"
	operationStepType = "CallGetGlobalQuote"
)

var demoOutcomeAttribute = dex.DefineAttribute[alphavantage.GlobalQuoteOutput]("alphavantage-demo-outcome")

// Flow runs GetGlobalQuote once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection alphavantage.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection alphavantage.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{demoOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(alphavantage.NewGetGlobalQuoteStep(alphavantage.GetGlobalQuoteStepConfig[alphavantage.StockInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Alpha Vantage", Explanation: "Call the provider's getGlobalQuote operation."},
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

// dex:field attribute-key:alphavantage-demo-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDemoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alphavantage-demo-outcome": outcome}}, nil
}

// dex:field attribute-key:alphavantage-demo-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDemoOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"alphavantage-demo-outcome": outcome}}, nil
}

func readDemoOutcome(ctx dex.Context) (alphavantage.GlobalQuoteOutput, error) {
	outcome, err := demoOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return alphavantage.GlobalQuoteOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input alphavantage.StockInput) alphavantage.StockInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetGlobalQuote" }
func (startRequest) WaitFor(dex.Context, alphavantage.StockInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input alphavantage.StockInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[alphavantage.StockInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[alphavantage.GetGlobalQuoteResult]
}

func (completeResult) GetStepType() string { return "CompleteGetGlobalQuote" }
func (completeResult) Execute(ctx dex.Context, result alphavantage.GetGlobalQuoteResult) (*dex.StepDecision, error) {
	if err := demoOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
