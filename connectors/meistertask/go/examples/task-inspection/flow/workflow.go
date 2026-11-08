// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskinspection runs MeisterTask getTask as one Dex Flow.
package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MeisterTaskTaskInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "meistertask"
	operationStepType = "CallGetTask"
)

var taskAttribute = dex.DefineAttribute[meistertask.TaskSummary]("meistertask-task")

// Flow runs GetTask once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection meistertask.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(meistertask.NewGetTaskStep(meistertask.GetTaskStepConfig[meistertask.GetTaskInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "Read a task from MeisterTask."},
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

// dex:field attribute-key:meistertask-task value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-task": outcome}}, nil
}

// dex:field attribute-key:meistertask-task value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-task": outcome}}, nil
}

func readTask(ctx dex.Context) (meistertask.TaskSummary, error) {
	outcome, err := taskAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.TaskSummary{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input meistertask.GetTaskInput) meistertask.GetTaskInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTask" }
func (startRequest) WaitFor(dex.Context, meistertask.GetTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input meistertask.GetTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.GetTaskInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[meistertask.GetTaskResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTask" }
func (completeResult) Execute(ctx dex.Context, result meistertask.GetTaskResult) (*dex.StepDecision, error) {
	if err := taskAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
