// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package instancelist runs OceanBase listInstances as one Dex Flow.
package instancelist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/oceanbase/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "OceanBaseInstanceList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "oceanbase"
	operationStepType = "CallListInstances"
)

var instanceListOutcomeAttribute = dex.DefineAttribute[oceanbase.ListInstancesOutput]("oceanbase-instance-list-outcome")

// Flow runs ListInstances once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection oceanbase.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection oceanbase.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{instanceListOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(oceanbase.NewListInstancesStep(oceanbase.ListInstancesStepConfig[oceanbase.ListInstancesInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "OceanBase", Explanation: "Call the provider's listInstances operation."},
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

// dex:field attribute-key:oceanbase-instance-list-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInstanceListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oceanbase-instance-list-outcome": outcome}}, nil
}

// dex:field attribute-key:oceanbase-instance-list-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInstanceListOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oceanbase-instance-list-outcome": outcome}}, nil
}

func readInstanceListOutcome(ctx dex.Context) (oceanbase.ListInstancesOutput, error) {
	outcome, err := instanceListOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return oceanbase.ListInstancesOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input oceanbase.ListInstancesInput) oceanbase.ListInstancesInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListInstances" }
func (startRequest) WaitFor(dex.Context, oceanbase.ListInstancesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input oceanbase.ListInstancesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[oceanbase.ListInstancesInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[oceanbase.ListInstancesResult]
}

func (completeResult) GetStepType() string { return "CompleteListInstances" }
func (completeResult) Execute(ctx dex.Context, result oceanbase.ListInstancesResult) (*dex.StepDecision, error) {
	if err := instanceListOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
