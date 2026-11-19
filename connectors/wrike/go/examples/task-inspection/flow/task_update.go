// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/wrike/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var updatedTaskAttribute = dex.DefineAttribute[wrike.TaskStatus]("wrike-updated-task")

const (
	// UpdateFlowType is the stable task update Flow identity.
	UpdateFlowType = "WrikeTaskUpdate"
	updateStepType = "CallUpdateTask"
)

// UpdateFlow replaces a task title or status.
type UpdateFlow struct {
	dex.FlowDefaults
	connection wrike.Connection
}

// NewUpdateFlow binds the provider connection at registration time.
func NewUpdateFlow(connection wrike.Connection) *UpdateFlow {
	return &UpdateFlow{connection: connection}
}

func (*UpdateFlow) GetFlowType() string { return UpdateFlowType }
func (*UpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedTaskAttribute}}
}
func (flow *UpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdate{}),
		dex.DefineStep(wrike.NewUpdateTaskStep(wrike.UpdateTaskStepConfig[wrike.UpdateTaskInput]{
			StepType: updateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Wrike", Explanation: "Replace the task title or status in Wrike."},
			Connection:  flow.connection, MapToOperationInput: mapUpdateInput,
			Updated: sdkgo.GoTo(completeUpdate{}),
		})),
		dex.DefineStep(completeUpdate{}),
	}
}
func (flow *UpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wrike-updated-task value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last task result in Dex Web.
func (*UpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	task, err := readUpdatedTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wrike-updated-task": task}}, nil
}

// dex:field attribute-key:wrike-updated-task value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last task result in Dex Web.
func (*UpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	task, err := readUpdatedTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wrike-updated-task": task}}, nil
}

func readUpdatedTask(ctx dex.Context) (wrike.TaskStatus, error) {
	task, err := updatedTaskAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wrike.TaskStatus{}, nil
	}
	return task, err
}

func mapUpdateInput(input wrike.UpdateTaskInput) wrike.UpdateTaskInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed task update to the provider Step."
type startUpdate struct{ dex.StepDefaults }

func (startUpdate) GetStepType() string { return "StartUpdateTask" }
func (startUpdate) WaitFor(dex.Context, wrike.UpdateTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdate) Execute(_ dex.Context, input wrike.UpdateTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wrike.UpdateTaskInput](updateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the updated task."
type completeUpdate struct {
	dex.StepDefaultsNoWaitFor[wrike.UpdateTaskResult]
}

func (completeUpdate) GetStepType() string { return "CompleteUpdateTask" }
func (completeUpdate) Execute(ctx dex.Context, result wrike.UpdateTaskResult) (*dex.StepDecision, error) {
	if err := updatedTaskAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateFlow)(nil)
