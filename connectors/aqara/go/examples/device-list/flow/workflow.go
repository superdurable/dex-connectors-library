// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package devicelist runs Aqara listDevices as one Dex Flow.
package devicelist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/aqara/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AqaraDevices"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "aqara"
	operationStepType = "CallListDevices"
)

var deviceListOutcomeAttribute = dex.DefineAttribute[aqara.DevicePage]("aqara-device-list-outcome")

// Flow runs ListDevices once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection aqara.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection aqara.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deviceListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(aqara.NewListDevicesStep(aqara.ListDevicesStepConfig[aqara.ListDevicesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Aqara", Explanation: "Read one page of authorized Aqara devices."},
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

// dex:field attribute-key:aqara-device-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDevicesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"aqara-device-list-outcome": outcome}}, nil
}

// dex:field attribute-key:aqara-device-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDevicesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"aqara-device-list-outcome": outcome}}, nil
}

func readDevicesOutcome(ctx dex.Context) (aqara.DevicePage, error) {
	outcome, err := deviceListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return aqara.DevicePage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input aqara.ListDevicesInput) aqara.ListDevicesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDevices" }
func (startRequest) WaitFor(dex.Context, aqara.ListDevicesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input aqara.ListDevicesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[aqara.ListDevicesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[aqara.ListDevicesResult]
}

func (completeResult) GetStepType() string { return "CompleteListDevices" }
func (completeResult) Execute(ctx dex.Context, result aqara.ListDevicesResult) (*dex.StepDecision, error) {
	if err := deviceListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
