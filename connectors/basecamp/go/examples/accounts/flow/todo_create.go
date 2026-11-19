// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TodoCreateFlowType identifies the to-do creation example.
const TodoCreateFlowType = "BasecampTodoCreate"

const todoCreateStepType = "CallCreateTodo"

var createdTodoAttribute = dex.DefineAttribute[basecamp.Todo]("basecamp-created-todo")

// TodoCreateFlow creates one Basecamp to-do.
type TodoCreateFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewTodoCreateFlow binds the Basecamp connection at registration.
func NewTodoCreateFlow(connection basecamp.Connection) *TodoCreateFlow {
	return &TodoCreateFlow{connection: connection}
}

func (*TodoCreateFlow) GetFlowType() string { return TodoCreateFlowType }

func (*TodoCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdTodoAttribute}}
}

// GetRPCs registers read-only to-do views.
func (flow *TodoCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-created-todo value-type:json editable:false description:"Created to-do"
// GetDexSummary shows the created to-do identity.
func (*TodoCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	todo, err := readCreatedTodo(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-todo": todo}}, nil
}

// dex:field attribute-key:basecamp-created-todo value-type:json editable:false description:"Created to-do"
// GetDexDisplay shows the created to-do identity.
func (*TodoCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	todo, err := readCreatedTodo(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-todo": todo}}, nil
}

func readCreatedTodo(ctx dex.Context) (basecamp.Todo, error) {
	todo, err := createdTodoAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.Todo{}, nil
	}
	return todo, err
}

func (flow *TodoCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTodoCreate{}),
		dex.DefineStep(basecamp.NewCreateTodoStep(basecamp.CreateTodoStepConfig[basecamp.CreateTodoInput]{
			StepType: todoCreateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Create one to-do in the selected list."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.CreateTodoInput) basecamp.CreateTodoInput { return input },
			Created:             sdkgo.GoTo(completeTodoCreate{}),
		})),
		dex.DefineStep(completeTodoCreate{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the to-do fields to the creation Step."
type startTodoCreate struct{ dex.StepDefaults }

func (startTodoCreate) GetStepType() string { return "StartCreateTodo" }
func (startTodoCreate) WaitFor(dex.Context, basecamp.CreateTodoInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTodoCreate) Execute(_ dex.Context, input basecamp.CreateTodoInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.CreateTodoInput](todoCreateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created to-do identity."
type completeTodoCreate struct {
	dex.StepDefaultsNoWaitFor[basecamp.CreateTodoResult]
}

func (completeTodoCreate) GetStepType() string { return "CompleteCreateTodo" }
func (completeTodoCreate) Execute(ctx dex.Context, result basecamp.CreateTodoResult) (*dex.StepDecision, error) {
	if err := createdTodoAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TodoCreateFlow)(nil)
