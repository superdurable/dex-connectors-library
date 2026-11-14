// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package usageread runs UPYUN getServiceUsage as one Dex Flow.
package usageread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/upyun/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "UPYUNUsageRead"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "upyun"
	operationStepType = "CallGetServiceUsage"
)

var usageAttribute = dex.DefineAttribute[upyun.ServiceUsage]("upyun-usage-read")

// Flow runs GetServiceUsage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection upyun.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection upyun.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{usageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(upyun.NewGetServiceUsageStep(upyun.GetServiceUsageStepConfig[upyun.GetServiceUsageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "UPYUN", Explanation: "Read the UPYUN storage service usage."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Read:                sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:upyun-usage-read value-type:json editable:false description:"Storage service usage"
// GetDexSummary shows the last storage service usage in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUsage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"upyun-usage-read": outcome}}, nil
}

// dex:field attribute-key:upyun-usage-read value-type:json editable:false description:"Storage service usage"
// GetDexDisplay shows the last storage service usage in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUsage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"upyun-usage-read": outcome}}, nil
}

func readUsage(ctx dex.Context) (upyun.ServiceUsage, error) {
	outcome, err := usageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return upyun.ServiceUsage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input upyun.GetServiceUsageInput) upyun.GetServiceUsageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetServiceUsage" }
func (startRequest) WaitFor(dex.Context, upyun.GetServiceUsageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input upyun.GetServiceUsageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[upyun.GetServiceUsageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[upyun.GetServiceUsageResult]
}

func (completeResult) GetStepType() string { return "CompleteGetServiceUsage" }
func (completeResult) Execute(ctx dex.Context, result upyun.GetServiceUsageResult) (*dex.StepDecision, error) {
	if err := usageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
