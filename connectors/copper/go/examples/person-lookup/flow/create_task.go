// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createTaskStepType = "CreateCopperTask"

var createdTaskAttribute = dex.DefineAttribute[copper.TaskMetadata]("copper-created-task")

// TaskCreationFlow creates one Copper task with bounded sends.
type TaskCreationFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewTaskCreationFlow binds the static Copper connection.
func NewTaskCreationFlow(connection copper.Connection) *TaskCreationFlow {
	return &TaskCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*TaskCreationFlow) GetFlowType() string { return "CopperTaskCreation" }

// GetPersistenceSchema retains the created task for Dex Web.
func (*TaskCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdTaskAttribute}}
}

// GetSteps wires the operation-specific task Mutation.
func (flow *TaskCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTaskCreation{}),
		dex.DefineStep(copper.NewCreateTaskStep(copper.CreateTaskStepConfig[copper.CreateTaskInput]{
			StepType: createTaskStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Create a Copper task."},
			Connection:  flow.connection, MapToOperationInput: mapTaskCreationInput,
			Created: sdkgo.GoTo(completeTaskCreation{}),
		})),
		dex.DefineStep(completeTaskCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *TaskCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-created-task value-type:json editable:false description:"Created task"
// GetDexSummary shows the created task in Dex Web.
func (*TaskCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTaskMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-task": page}}, nil
}

// dex:field attribute-key:copper-created-task value-type:json editable:false description:"Created task"
// GetDexDisplay shows the created task in Dex Web.
func (*TaskCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTaskMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-task": page}}, nil
}

func readTaskMetadata(ctx dex.Context) (copper.TaskMetadata, error) {
	page, err := createdTaskAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.TaskMetadata{}, nil
	}
	return page, err
}

func mapTaskCreationInput(input copper.CreateTaskInput) copper.CreateTaskInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested task name to Copper."
type startTaskCreation struct{ dex.StepDefaults }

func (startTaskCreation) WaitFor(dex.Context, copper.CreateTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTaskCreation) Execute(_ dex.Context, input copper.CreateTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.CreateTaskInput](createTaskStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created task and complete the Flow."
type completeTaskCreation struct {
	dex.StepDefaultsNoWaitFor[copper.CreateTaskResult]
}

func (completeTaskCreation) Execute(ctx dex.Context, result copper.CreateTaskResult) (*dex.StepDecision, error) {
	if err := createdTaskAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TaskCreationFlow)(nil)
