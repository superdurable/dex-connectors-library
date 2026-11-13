// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package tradingday runs Tushare getTradingDay as one Dex Flow.
package tradingday

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tushare/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TushareTradingDay"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tushare"
	operationStepType = "CallGetTradingDay"
)

var tradingDayOutcomeAttribute = dex.DefineAttribute[tushare.GetTradingDayOutput]("tushare-get-trading-day-outcome")

// Flow runs GetTradingDay once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tushare.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tushare.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tradingDayOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tushare.NewGetTradingDayStep(tushare.GetTradingDayStepConfig[tushare.GetTradingDayInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Tushare", Explanation: "Read one securities exchange trading date from Tushare Pro."},
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

// dex:field attribute-key:tushare-get-trading-day-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTradingDayOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tushare-get-trading-day-outcome": outcome}}, nil
}

// dex:field attribute-key:tushare-get-trading-day-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetTradingDayOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tushare-get-trading-day-outcome": outcome}}, nil
}

func readGetTradingDayOutcome(ctx dex.Context) (tushare.GetTradingDayOutput, error) {
	outcome, err := tradingDayOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tushare.GetTradingDayOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tushare.GetTradingDayInput) tushare.GetTradingDayInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the exchange and date to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTradingDay" }
func (startRequest) WaitFor(dex.Context, tushare.GetTradingDayInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tushare.GetTradingDayInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tushare.GetTradingDayInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the trading date result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tushare.GetTradingDayResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTradingDay" }
func (completeResult) Execute(ctx dex.Context, result tushare.GetTradingDayResult) (*dex.StepDecision, error) {
	if err := tradingDayOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
