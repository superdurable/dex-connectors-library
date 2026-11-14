// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package regionlist runs UCloud getRegion as one Dex Flow.
package regionlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/ucloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "UCloudRegionList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ucloud"
	operationStepType = "CallGetRegion"
)

var regionListOutcomeAttribute = dex.DefineAttribute[ucloud.GetRegionOutput]("ucloud-region-list-outcome")

// Flow runs GetRegion once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ucloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ucloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{regionListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ucloud.NewGetRegionStep(ucloud.GetRegionStepConfig[ucloud.GetRegionInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "UCloud", Explanation: "Call the provider's getRegion operation."},
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

// dex:field attribute-key:ucloud-region-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRegionListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ucloud-region-list-outcome": outcome}}, nil
}

// dex:field attribute-key:ucloud-region-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readRegionListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ucloud-region-list-outcome": outcome}}, nil
}

func readRegionListOutcome(ctx dex.Context) (ucloud.GetRegionOutput, error) {
	outcome, err := regionListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ucloud.GetRegionOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ucloud.GetRegionInput) ucloud.GetRegionInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetRegion" }
func (startRequest) WaitFor(dex.Context, ucloud.GetRegionInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ucloud.GetRegionInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ucloud.GetRegionInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ucloud.GetRegionResult]
}

func (completeResult) GetStepType() string { return "CompleteGetRegion" }
func (completeResult) Execute(ctx dex.Context, result ucloud.GetRegionResult) (*dex.StepDecision, error) {
	if err := regionListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
