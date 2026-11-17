// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createTaskStepType = "CallCreateTask"

var createdTaskAttribute = dex.DefineAttribute[teamwork.TaskSummary]("teamwork-created-task")

// CreateTaskFlow creates a task through the static Teamwork connection.
type CreateTaskFlow struct {
	dex.FlowDefaults
	connection teamwork.Connection
}

// NewCreateTaskFlow binds the Teamwork connection at Worker registration.
func NewCreateTaskFlow(connection teamwork.Connection) *CreateTaskFlow {
	return &CreateTaskFlow{connection: connection}
}

// GetFlowType returns the stable task-create Flow identity.
func (*CreateTaskFlow) GetFlowType() string { return "TeamworkCreateTask" }

// GetPersistenceSchema registers created task metadata.
func (*CreateTaskFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdTaskAttribute}}
}

// GetSteps registers the create and completion Steps.
func (flow *CreateTaskFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginCreateTask{}),
		dex.DefineStep(teamwork.NewCreateTaskStep(teamwork.CreateTaskStepConfig[teamwork.CreateTaskInput]{
			StepType: createTaskStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Teamwork", Explanation: "Create a named task in a task list."},
			MapToOperationInput: mapCreateTaskInput, Created: sdkgo.GoTo(completeCreateTask{}),
		})),
		dex.DefineStep(completeCreateTask{}),
	}
}

// GetRPCs exposes read-only Dex Web task metadata.
func (flow *CreateTaskFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the created task when available.
// dex:field attribute-key:teamwork-created-task value-type:json editable:false description:"Created task"
func (*CreateTaskFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	task, err := readCreatedTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-created-task": task}}, nil
}

// GetDexDisplay returns the created task when available.
// dex:field attribute-key:teamwork-created-task value-type:json editable:false description:"Created task"
func (*CreateTaskFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	task, err := readCreatedTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-created-task": task}}, nil
}

func readCreatedTask(ctx dex.Context) (teamwork.TaskSummary, error) {
	task, err := createdTaskAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return teamwork.TaskSummary{}, nil
	}
	return task, err
}

func mapCreateTaskInput(input teamwork.CreateTaskInput) teamwork.CreateTaskInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the task list ID and name to the provider Step."
type beginCreateTask struct{ dex.StepDefaults }

func (beginCreateTask) WaitFor(dex.Context, teamwork.CreateTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginCreateTask) Execute(_ dex.Context, input teamwork.CreateTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[teamwork.CreateTaskInput](createTaskStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created task metadata."
type completeCreateTask struct {
	dex.StepDefaultsNoWaitFor[teamwork.CreateTaskResult]
}

func (completeCreateTask) Execute(ctx dex.Context, result teamwork.CreateTaskResult) (*dex.StepDecision, error) {
	if err := createdTaskAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateTaskFlow)(nil)
