// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package currentweather runs Seniverse getCurrentWeather as one Dex Flow.
package currentweather

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/seniverse/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "SeniverseCurrentWeather"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "seniverse"
	operationStepType = "CallGetCurrentWeather"
)

var currentWeatherOutcomeAttribute = dex.DefineAttribute[seniverse.GetCurrentWeatherOutput]("seniverse-current-weather-outcome")

// Flow runs GetCurrentWeather once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection seniverse.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection seniverse.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{currentWeatherOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(seniverse.NewGetCurrentWeatherStep(seniverse.GetCurrentWeatherStepConfig[seniverse.GetCurrentWeatherInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Seniverse", Explanation: "Read the current weather for the requested city."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Fetched:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:seniverse-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"seniverse-current-weather-outcome": outcome}}, nil
}

// dex:field attribute-key:seniverse-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"seniverse-current-weather-outcome": outcome}}, nil
}

func readCurrentWeatherOutcome(ctx dex.Context) (seniverse.GetCurrentWeatherOutput, error) {
	outcome, err := currentWeatherOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return seniverse.GetCurrentWeatherOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input seniverse.GetCurrentWeatherInput) seniverse.GetCurrentWeatherInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCurrentWeather" }
func (startRequest) WaitFor(dex.Context, seniverse.GetCurrentWeatherInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input seniverse.GetCurrentWeatherInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[seniverse.GetCurrentWeatherInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[seniverse.GetCurrentWeatherResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCurrentWeather" }
func (completeResult) Execute(ctx dex.Context, result seniverse.GetCurrentWeatherResult) (*dex.StepDecision, error) {
	if err := currentWeatherOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
