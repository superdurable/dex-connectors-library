// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// GetTaskFlowType is the stable identity for a single task read.
const GetTaskFlowType = "MicrosoftTodoGetTask"

// GetTaskFlow reads one task selected by its list and task IDs.
type GetTaskFlow struct {
	dex.FlowDefaults
	connection todo.Connection
}

// NewGetTaskFlow binds the To Do connection at registration.
func NewGetTaskFlow(connection todo.Connection) *GetTaskFlow {
	return &GetTaskFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*GetTaskFlow) GetFlowType() string { return GetTaskFlowType }

// GetPersistenceSchema declares no persistent Attributes or Streams.
func (*GetTaskFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetRPCs exposes the Flow status in Dex Web.
func (flow *GetTaskFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary describes where to find the completed task result.
func (*GetTaskFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay describes where to find the completed task result.
func (*GetTaskFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetSteps declares the start, provider, and completion Steps.
func (flow *GetTaskFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startGetTask{}),
		dex.DefineStep(todo.NewGetTaskStep(todo.GetTaskStepConfig[todo.GetTaskInput]{StepType: "CallGetTask", ConnectionName: ConnectionName, Connection: flow.connection, MapToOperationInput: mapGetTaskInput, Retrieved: sdkgo.GoTo(completeGetTask{}), Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft To Do", Explanation: "Read the selected To Do task."}})),
		dex.DefineStep(completeGetTask{}),
	}
}

func mapGetTaskInput(input todo.GetTaskInput) todo.GetTaskInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start the selected task query."
type startGetTask struct{ dex.StepDefaults }

func (startGetTask) GetStepType() string { return "StartGetTask" }
func (startGetTask) WaitFor(dex.Context, todo.GetTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startGetTask) Execute(_ dex.Context, input todo.GetTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todo.GetTaskInput]("CallGetTask"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected task."
type completeGetTask struct {
	dex.StepDefaultsNoWaitFor[todo.GetTaskResult]
}

func (completeGetTask) GetStepType() string { return "CompleteGetTask" }
func (completeGetTask) Execute(_ dex.Context, result todo.GetTaskResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetTaskFlow)(nil)
