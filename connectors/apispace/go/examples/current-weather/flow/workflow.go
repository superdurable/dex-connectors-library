// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package currentweather runs APISpace getCurrentWeather as one Dex Flow.
package currentweather

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/apispace/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "APISpaceCurrentWeather"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "apispace"
	operationStepType = "CallGetCurrentWeather"
)

var currentWeatherOutcomeAttribute = dex.DefineAttribute[apispace.CurrentWeather]("apispace-current-weather-outcome")

// Flow runs GetCurrentWeather once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection apispace.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection apispace.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{currentWeatherOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(apispace.NewGetCurrentWeatherStep(apispace.GetCurrentWeatherStepConfig[apispace.GetCurrentWeatherInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "APISpace", Explanation: "Read APISpace current city weather."},
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

// dex:field attribute-key:apispace-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apispace-current-weather-outcome": outcome}}, nil
}

// dex:field attribute-key:apispace-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"apispace-current-weather-outcome": outcome}}, nil
}

func readCurrentWeatherOutcome(ctx dex.Context) (apispace.CurrentWeather, error) {
	outcome, err := currentWeatherOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return apispace.CurrentWeather{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input apispace.GetCurrentWeatherInput) apispace.GetCurrentWeatherInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCurrentWeather" }
func (startRequest) WaitFor(dex.Context, apispace.GetCurrentWeatherInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input apispace.GetCurrentWeatherInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[apispace.GetCurrentWeatherInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[apispace.GetCurrentWeatherResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCurrentWeather" }
func (completeResult) Execute(ctx dex.Context, result apispace.GetCurrentWeatherResult) (*dex.StepDecision, error) {
	if err := currentWeatherOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
