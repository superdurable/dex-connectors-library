// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskinspection runs Microsoft To Do list and task queries as Dex Flows.
package listinspection

import (
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// ListFlowType is the stable task-list inspection Flow identity.
	ListFlowType = "MicrosoftTodoListInspection"
	// ConnectionName is the static Dex Web connection for both Flows.
	ConnectionName = "microsoft-todo"
)

// ListFlow reads all bounded task lists for the signed-in account.
type ListFlow struct {
	dex.FlowDefaults
	connection todo.Connection
}

// NewListFlow binds the To Do connection at registration.
func NewListFlow(connection todo.Connection) *ListFlow { return &ListFlow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*ListFlow) GetFlowType() string { return ListFlowType }

// GetPersistenceSchema declares no persistent Attributes or Streams.
func (*ListFlow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// GetSteps declares the start, provider, and completion Steps.
// GetRPCs exposes the Flow status in Dex Web.
func (flow *ListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary describes where to find the completed provider result.
func (*ListFlow) GetDexSummary(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

// GetDexDisplay describes where to find the completed provider result.
func (*ListFlow) GetDexDisplay(_ dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	return &dex.RPCResult[map[string]any]{Output: map[string]any{}}, nil
}

func (flow *ListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLists{}),
		dex.DefineStep(todo.NewListTaskListsStep(todo.ListTaskListsStepConfig[todo.ListTaskListsInput]{StepType: "CallListTaskLists", ConnectionName: ConnectionName, Connection: flow.connection, MapToOperationInput: mapListInput, Listed: sdkgo.GoTo(completeLists{}), Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Microsoft To Do", Explanation: "Read the signed-in user's To Do lists."}})),
		dex.DefineStep(completeLists{}),
	}
}

// mapListInput passes the typed empty request through the provider Step.
func mapListInput(input todo.ListTaskListsInput) todo.ListTaskListsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Start the task-list query."
type startLists struct{ dex.StepDefaults }

func (startLists) GetStepType() string { return "StartListTaskLists" }
func (startLists) WaitFor(dex.Context, todo.ListTaskListsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLists) Execute(_ dex.Context, input todo.ListTaskListsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[todo.ListTaskListsInput]("CallListTaskLists"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the task lists."
type completeLists struct {
	dex.StepDefaultsNoWaitFor[todo.ListTaskListsResult]
}

func (completeLists) GetStepType() string { return "CompleteListTaskLists" }
func (completeLists) Execute(_ dex.Context, result todo.ListTaskListsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListFlow)(nil)
