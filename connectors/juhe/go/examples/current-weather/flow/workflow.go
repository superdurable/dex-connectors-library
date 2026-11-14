// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package currentweather runs Juhe getCurrentWeather as one Dex Flow.
package currentweather

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/juhe/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JuheCurrentWeather"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "juhe"
	operationStepType = "CallGetCurrentWeather"
)

var currentWeatherOutcomeAttribute = dex.DefineAttribute[juhe.GetCurrentWeatherOutput]("juhe-current-weather-outcome")

// Flow runs GetCurrentWeather once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection juhe.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection juhe.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{currentWeatherOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(juhe.NewGetCurrentWeatherStep(juhe.GetCurrentWeatherStepConfig[juhe.GetCurrentWeatherInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Juhe", Explanation: "Read current weather conditions for the requested city."},
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

// dex:field attribute-key:juhe-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"juhe-current-weather-outcome": outcome}}, nil
}

// dex:field attribute-key:juhe-current-weather-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCurrentWeatherOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"juhe-current-weather-outcome": outcome}}, nil
}

func readCurrentWeatherOutcome(ctx dex.Context) (juhe.GetCurrentWeatherOutput, error) {
	outcome, err := currentWeatherOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return juhe.GetCurrentWeatherOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input juhe.GetCurrentWeatherInput) juhe.GetCurrentWeatherInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCurrentWeather" }
func (startRequest) WaitFor(dex.Context, juhe.GetCurrentWeatherInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input juhe.GetCurrentWeatherInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[juhe.GetCurrentWeatherInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[juhe.GetCurrentWeatherResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCurrentWeather" }
func (completeResult) Execute(ctx dex.Context, result juhe.GetCurrentWeatherResult) (*dex.StepDecision, error) {
	if err := currentWeatherOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
