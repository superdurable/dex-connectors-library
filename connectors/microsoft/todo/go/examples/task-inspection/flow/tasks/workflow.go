// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskinspection runs Microsoft To Do list and task queries as Dex Flows.
package taskinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// TaskFlowType is the stable task inspection Flow identity.
	TaskFlowType = "MicrosoftTodoTaskInspection"
	// ConnectionName is the static Dex Web connection.
	ConnectionName = "microsoft-todo"
)

// TaskFlow reads tasks from one list selected by stable list ID.
type TaskFlow struct {
	dex.FlowDefaults
	connection todo.Connection
}

// NewTaskFlow binds the To Do connection at registration.
func NewTaskFlow(connection todo.Connection) *TaskFlow { return &TaskFlow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*TaskFlow) GetFlowType() string { return TaskFlowType }

// GetPersistenceSchema declares no persistent Attributes or Streams.
func (*TaskFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetSteps declares the start, provider, and completion Steps.
// GetRPCs exposes the Flow status in Dex Web.
func (flow *TaskFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary describes where to find the completed provider result.
func (*TaskFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay describes where to find the completed provider result.
func (*TaskFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *TaskFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTasks{}),
		dex.DefineStep(todo.NewListTasksStep(todo.ListTasksStepConfig[todo.ListTasksInput]{StepType: "CallListTasks", ConnectionName: ConnectionName, Connection: flow.connection, MapToOperationInput: mapTaskInput, Listed: sdkgo.GoTo(completeTasks{}), Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft To Do", Explanation: "Read tasks in the selected To Do list."}})),
		dex.DefineStep(completeTasks{}),
	}
}

// mapTaskInput passes the selected list ID to the provider Step.
func mapTaskInput(input todo.ListTasksInput) todo.ListTasksInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start the selected task-list query."
type startTasks struct{ dex.StepDefaults }

func (startTasks) GetStepType() string { return "StartListTasks" }
func (startTasks) WaitFor(dex.Context, todo.ListTasksInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTasks) Execute(_ dex.Context, input todo.ListTasksInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todo.ListTasksInput]("CallListTasks"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected list's tasks."
type completeTasks struct {
	dex.StepDefaultsNoWaitFor[todo.ListTasksResult]
}

func (completeTasks) GetStepType() string { return "CompleteListTasks" }
func (completeTasks) Execute(_ dex.Context, result todo.ListTasksResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TaskFlow)(nil)
