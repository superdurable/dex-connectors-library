// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listTasksStepType = "ListCopperTasks"

var tasksPageAttribute = dex.DefineAttribute[copper.TasksPage]("copper-tasks-page")

// TasksSearchFlow reads one bounded page of Copper tasks.
type TasksSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewTasksSearchFlow binds the static Copper connection.
func NewTasksSearchFlow(connection copper.Connection) *TasksSearchFlow {
	return &TasksSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*TasksSearchFlow) GetFlowType() string { return "CopperTaskSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*TasksSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tasksPageAttribute}}
}

// GetSteps wires the operation-specific tasks Query.
func (flow *TasksSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTasksSearch{}),
		dex.DefineStep(copper.NewListTasksStep(copper.ListTasksStepConfig[copper.ListTasksInput]{
			StepType: listTasksStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper tasks."},
			Connection:  flow.connection, MapToOperationInput: mapTasksSearchInput,
			Listed: sdkgo.GoTo(completeTasksSearch{}),
		})),
		dex.DefineStep(completeTasksSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *TasksSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-tasks-page value-type:json editable:false description:"Tasks search page"
// GetDexSummary shows the returned page in Dex Web.
func (*TasksSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTasksPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-tasks-page": page}}, nil
}

// dex:field attribute-key:copper-tasks-page value-type:json editable:false description:"Tasks search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*TasksSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTasksPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-tasks-page": page}}, nil
}

func readTasksPage(ctx dex.Context) (copper.TasksPage, error) {
	page, err := tasksPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.TasksPage{}, nil
	}
	return page, err
}

func mapTasksSearchInput(input copper.ListTasksInput) copper.ListTasksInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested tasks page and filters to Copper."
type startTasksSearch struct{ dex.StepDefaults }

func (startTasksSearch) WaitFor(dex.Context, copper.ListTasksInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTasksSearch) Execute(_ dex.Context, input copper.ListTasksInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListTasksInput](listTasksStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the tasks page and complete the Flow."
type completeTasksSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListTasksResult]
}

func (completeTasksSearch) Execute(ctx dex.Context, result copper.ListTasksResult) (*dex.StepDecision, error) {
	if err := tasksPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TasksSearchFlow)(nil)
