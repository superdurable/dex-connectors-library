// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package flightstatus runs VariFlight getFlightStatus as one Dex Flow.
package flightstatus

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/variflight/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "VariFlightFlightStatus"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "variflight"
	operationStepType = "CallGetFlightStatus"
)

var flightStatusOutcomeAttribute = dex.DefineAttribute[variflight.GetFlightStatusOutput]("variflight-flight-status-outcome")

// Flow runs GetFlightStatus once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection variflight.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection variflight.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{flightStatusOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(variflight.NewGetFlightStatusStep(variflight.GetFlightStatusStepConfig[variflight.GetFlightStatusInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "VariFlight", Explanation: "Read a signed flight-status result for the number and date."},
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

// dex:field attribute-key:variflight-flight-status-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetFlightStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"variflight-flight-status-outcome": outcome}}, nil
}

// dex:field attribute-key:variflight-flight-status-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readGetFlightStatusOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"variflight-flight-status-outcome": outcome}}, nil
}

func readGetFlightStatusOutcome(ctx dex.Context) (variflight.GetFlightStatusOutput, error) {
	outcome, err := flightStatusOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return variflight.GetFlightStatusOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input variflight.GetFlightStatusInput) variflight.GetFlightStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetFlightStatus" }
func (startRequest) WaitFor(dex.Context, variflight.GetFlightStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input variflight.GetFlightStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[variflight.GetFlightStatusInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[variflight.GetFlightStatusResult]
}

func (completeResult) GetStepType() string { return "CompleteGetFlightStatus" }
func (completeResult) Execute(ctx dex.Context, result variflight.GetFlightStatusResult) (*dex.StepDecision, error) {
	if err := flightStatusOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
