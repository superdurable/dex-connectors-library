// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const taskListingStepType = "CallListTasks"

var taskListingAttribute = dex.DefineAttribute[meistertask.TasksPage]("meistertask-task-list")

// TaskListingFlow reads one page of tasks for a Dex application.
type TaskListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewTaskListingFlow binds the same static MeisterTask connection used by task inspection.
func NewTaskListingFlow(connection meistertask.Connection) *TaskListingFlow {
	return &TaskListingFlow{connection: connection}
}

// GetFlowType identifies the task listing Flow.
func (*TaskListingFlow) GetFlowType() string { return "MeisterTaskTaskListing" }

// GetPersistenceSchema registers its durable provider result.
func (*TaskListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskListingAttribute}}
}

// GetSteps wires the task listing Query through an operation-specific factory.
func (flow *TaskListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTaskListing{}),
		dex.DefineStep(meistertask.NewListTasksStep(meistertask.ListTasksStepConfig[meistertask.ListTasksInput]{
			StepType: taskListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List tasks visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listTasksInput,
			Listed: sdkgo.GoTo(completeTaskListing{}),
		})),
		dex.DefineStep(completeTaskListing{}),
	}
}

// GetRPCs exposes the task page in Dex Web.
func (flow *TaskListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-task-list value-type:json editable:false description:"Task page"
// GetDexSummary returns the last task page.
func (*TaskListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tasks, err := readTaskListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-task-list": tasks}}, nil
}

// dex:field attribute-key:meistertask-task-list value-type:json editable:false description:"Task page"
// GetDexDisplay returns the last task page.
func (*TaskListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	tasks, err := readTaskListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-task-list": tasks}}, nil
}

func readTaskListing(ctx dex.Context) (meistertask.TasksPage, error) {
	tasks, err := taskListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.TasksPage{}, nil
	}
	return tasks, err
}

func listTasksInput(input meistertask.ListTasksInput) meistertask.ListTasksInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startTaskListing struct{ dex.StepDefaults }

func (startTaskListing) GetStepType() string { return "StartTaskListing" }
func (startTaskListing) WaitFor(dex.Context, meistertask.ListTasksInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTaskListing) Execute(_ dex.Context, input meistertask.ListTasksInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListTasksInput](taskListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned task page and complete the Flow."
type completeTaskListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListTasksResult]
}

func (completeTaskListing) GetStepType() string { return "CompleteTaskListing" }
func (completeTaskListing) Execute(ctx dex.Context, result meistertask.ListTasksResult) (*dex.StepDecision, error) {
	if err := taskListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TaskListingFlow)(nil)
