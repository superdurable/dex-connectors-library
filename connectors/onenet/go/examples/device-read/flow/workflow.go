// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package deviceread runs OneNET queryDeviceDetail as one Dex Flow.
package deviceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/onenet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OneNETDeviceRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "onenet"
	operationStepType = "CallQueryDeviceDetail"
)

var deviceAttribute = dex.DefineAttribute[onenet.DeviceDetail]("onenet-device-read")

// Flow runs QueryDeviceDetail once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection onenet.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection onenet.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{deviceAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(onenet.NewQueryDeviceDetailStep(onenet.QueryDeviceDetailStepConfig[onenet.QueryDeviceDetailInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OneNET", Explanation: "Read OneNET device detail."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:onenet-device-read value-type:json editable:false description:"OneNET device detail"
// GetDexSummary shows the last OneNET device detail in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDevice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onenet-device-read": outcome}}, nil
}

// dex:field attribute-key:onenet-device-read value-type:json editable:false description:"OneNET device detail"
// GetDexDisplay shows the last OneNET device detail in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDevice(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"onenet-device-read": outcome}}, nil
}

func readDevice(ctx dex.Context) (onenet.DeviceDetail, error) {
	outcome, err := deviceAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return onenet.DeviceDetail{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input onenet.QueryDeviceDetailInput) onenet.QueryDeviceDetailInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartQueryDeviceDetail" }
func (startRequest) WaitFor(dex.Context, onenet.QueryDeviceDetailInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input onenet.QueryDeviceDetailInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[onenet.QueryDeviceDetailInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[onenet.QueryDeviceDetailResult]
}

func (completeResult) GetStepType() string { return "CompleteQueryDeviceDetail" }
func (completeResult) Execute(ctx dex.Context, result onenet.QueryDeviceDetailResult) (*dex.StepDecision, error) {
	if err := deviceAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
