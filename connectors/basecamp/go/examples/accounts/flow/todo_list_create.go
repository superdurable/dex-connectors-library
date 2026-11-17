// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TodoListCreateFlowType identifies the to-do list creation example.
const TodoListCreateFlowType = "BasecampTodoListCreate"

const todoListCreateStepType = "CallCreateTodoList"

var createdTodoListAttribute = dex.DefineAttribute[basecamp.TodoList]("basecamp-created-todo-list")

// TodoListCreateFlow creates one Basecamp to-do list.
type TodoListCreateFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewTodoListCreateFlow binds the provider connection at registration.
func NewTodoListCreateFlow(connection basecamp.Connection) *TodoListCreateFlow {
	return &TodoListCreateFlow{connection: connection}
}

func (*TodoListCreateFlow) GetFlowType() string { return TodoListCreateFlowType }

func (*TodoListCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdTodoListAttribute}}
}

// GetRPCs registers read-only to-do list views.
func (flow *TodoListCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-created-todo-list value-type:json editable:false description:"Created to-do list"
// GetDexSummary shows the created list identity.
func (*TodoListCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedTodoList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-todo-list": list}}, nil
}

// dex:field attribute-key:basecamp-created-todo-list value-type:json editable:false description:"Created to-do list"
// GetDexDisplay shows the created list identity.
func (*TodoListCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	list, err := readCreatedTodoList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-todo-list": list}}, nil
}

func readCreatedTodoList(ctx dex.Context) (basecamp.TodoList, error) {
	list, err := createdTodoListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.TodoList{}, nil
	}
	return list, err
}

func (flow *TodoListCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTodoListCreate{}),
		dex.DefineStep(basecamp.NewCreateTodoListStep(basecamp.CreateTodoListStepConfig[basecamp.CreateTodoListInput]{
			StepType: todoListCreateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Create one to-do list in the selected to-do set."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.CreateTodoListInput) basecamp.CreateTodoListInput { return input },
			Created:             sdkgo.GoTo(completeTodoListCreate{}),
		})),
		dex.DefineStep(completeTodoListCreate{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the list fields to the creation Step."
type startTodoListCreate struct{ dex.StepDefaults }

func (startTodoListCreate) GetStepType() string { return "StartCreateTodoList" }
func (startTodoListCreate) WaitFor(dex.Context, basecamp.CreateTodoListInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTodoListCreate) Execute(_ dex.Context, input basecamp.CreateTodoListInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.CreateTodoListInput](todoListCreateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created list identity."
type completeTodoListCreate struct {
	dex.StepDefaultsNoWaitFor[basecamp.CreateTodoListResult]
}

func (completeTodoListCreate) GetStepType() string { return "CompleteCreateTodoList" }
func (completeTodoListCreate) Execute(ctx dex.Context, result basecamp.CreateTodoListResult) (*dex.StepDecision, error) {
	if err := createdTodoListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TodoListCreateFlow)(nil)
