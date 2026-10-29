// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package demo runs OpenWeatherMap getCurrentWeather as one Dex Flow.
package demo

import (
	"github.com/superdurable/dex-connectors-library/connectors/openweathermap"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OpenWeatherMapDemo"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "openweathermap"
	operationStepType = "CallGetCurrentWeather"
)

// Flow runs GetCurrentWeather once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection openweathermap.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection openweathermap.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(openweathermap.NewGetCurrentWeatherStep(openweathermap.GetCurrentWeatherStepConfig[openweathermap.WeatherInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OpenWeatherMap", Explanation: "Call the provider's getCurrentWeather operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input openweathermap.WeatherInput) openweathermap.WeatherInput { return input }

// dex:group group-id:demo group-label:"Demo"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCurrentWeather" }
func (startRequest) WaitFor(dex.Context, openweathermap.WeatherInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input openweathermap.WeatherInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[openweathermap.WeatherInput](operationStepType), input), nil
}

// dex:group group-id:demo group-label:"Demo"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[openweathermap.GetCurrentWeatherResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCurrentWeather" }
func (completeResult) Execute(_ dex.Context, result openweathermap.GetCurrentWeatherResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
