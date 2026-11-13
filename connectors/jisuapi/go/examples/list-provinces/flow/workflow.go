// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package provinceslist runs JisuAPI listProvinces as one Dex Flow.
package provinceslist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/jisuapi/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JisuAPIProvinces"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jisuapi"
	operationStepType = "CallListProvinces"
)

var provincesOutcomeAttribute = dex.DefineAttribute[jisuapi.ListProvincesOutput]("jisuapi-list-provinces-outcome")

// Flow runs ListProvinces once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jisuapi.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jisuapi.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{provincesOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jisuapi.NewListProvincesStep(jisuapi.ListProvincesStepConfig[jisuapi.ListProvincesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "JisuAPI", Explanation: "Call the provider's listProvinces operation."},
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

// dex:field attribute-key:jisuapi-list-provinces-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProvincesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jisuapi-list-provinces-outcome": outcome}}, nil
}

// dex:field attribute-key:jisuapi-list-provinces-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProvincesOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jisuapi-list-provinces-outcome": outcome}}, nil
}

func readListProvincesOutcome(ctx dex.Context) (jisuapi.ListProvincesOutput, error) {
	outcome, err := provincesOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jisuapi.ListProvincesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jisuapi.ListProvincesInput) jisuapi.ListProvincesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListProvinces" }
func (startRequest) WaitFor(dex.Context, jisuapi.ListProvincesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jisuapi.ListProvincesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jisuapi.ListProvincesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jisuapi.ListProvincesResult]
}

func (completeResult) GetStepType() string { return "CompleteListProvinces" }
func (completeResult) Execute(ctx dex.Context, result jisuapi.ListProvincesResult) (*dex.StepDecision, error) {
	if err := provincesOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
