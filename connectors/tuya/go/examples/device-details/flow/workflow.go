// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package devicedetails runs Tuya getDeviceDetails as one Dex Flow.
package devicedetails

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/tuya/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TuyaDeviceDetails"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "tuya"
	operationStepType = "CallGetDeviceDetails"
)

var deviceDetailsOutcomeAttribute = dex.DefineAttribute[tuya.GetDeviceDetailsOutput]("tuya-device-details-outcome")

// Flow runs GetDeviceDetails once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection tuya.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection tuya.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deviceDetailsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(tuya.NewGetDeviceDetailsStep(tuya.GetDeviceDetailsStepConfig[tuya.GetDeviceDetailsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Tuya", Explanation: "Call the provider's getDeviceDetails operation."},
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

// dex:field attribute-key:tuya-device-details-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDeviceDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tuya-device-details-outcome": outcome}}, nil
}

// dex:field attribute-key:tuya-device-details-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDeviceDetailsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tuya-device-details-outcome": outcome}}, nil
}

func readDeviceDetailsOutcome(ctx dex.Context) (tuya.GetDeviceDetailsOutput, error) {
	outcome, err := deviceDetailsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tuya.GetDeviceDetailsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input tuya.GetDeviceDetailsInput) tuya.GetDeviceDetailsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetDeviceDetails" }
func (startRequest) WaitFor(dex.Context, tuya.GetDeviceDetailsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input tuya.GetDeviceDetailsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tuya.GetDeviceDetailsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[tuya.GetDeviceDetailsResult]
}

func (completeResult) GetStepType() string { return "CompleteGetDeviceDetails" }
func (completeResult) Execute(ctx dex.Context, result tuya.GetDeviceDetailsResult) (*dex.StepDecision, error) {
	if err := deviceDetailsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
