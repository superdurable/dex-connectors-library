// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package taskinspection runs Teamwork getTask as one Dex Flow.
package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "TeamworkTaskInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "teamwork"
	operationStepType = "CallGetTask"
)

var taskAttribute = dex.DefineAttribute[teamwork.TaskSummary]("teamwork-task")

// Flow runs GetTask once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection teamwork.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection teamwork.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taskAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(teamwork.NewGetTaskStep(teamwork.GetTaskStepConfig[teamwork.GetTaskInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Teamwork", Explanation: "Read a task from Teamwork."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:teamwork-task value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-task": outcome}}, nil
}

// dex:field attribute-key:teamwork-task value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readTask(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-task": outcome}}, nil
}

func readTask(ctx dex.Context) (teamwork.TaskSummary, error) {
	outcome, err := taskAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return teamwork.TaskSummary{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input teamwork.GetTaskInput) teamwork.GetTaskInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetTask" }
func (startRequest) WaitFor(dex.Context, teamwork.GetTaskInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input teamwork.GetTaskInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[teamwork.GetTaskInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[teamwork.GetTaskResult]
}

func (completeResult) GetStepType() string { return "CompleteGetTask" }
func (completeResult) Execute(ctx dex.Context, result teamwork.GetTaskResult) (*dex.StepDecision, error) {
	if err := taskAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
