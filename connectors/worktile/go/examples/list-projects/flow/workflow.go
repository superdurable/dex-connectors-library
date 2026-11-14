// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package listprojects runs Worktile listProjects as one Dex Flow.
package listprojects

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/worktile/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WorktileProjectList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "worktile"
	operationStepType = "CallListProjects"
)

var projectsOutcomeAttribute = dex.DefineAttribute[worktile.ListProjectsOutput]("worktile-list-projects-outcome")

// Flow runs ListProjects once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection worktile.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection worktile.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectsOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(worktile.NewListProjectsStep(worktile.ListProjectsStepConfig[worktile.ListProjectsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Worktile", Explanation: "Call the provider's listProjects operation."},
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

// dex:field attribute-key:worktile-list-projects-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProjectsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"worktile-list-projects-outcome": outcome}}, nil
}

// dex:field attribute-key:worktile-list-projects-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListProjectsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"worktile-list-projects-outcome": outcome}}, nil
}

func readListProjectsOutcome(ctx dex.Context) (worktile.ListProjectsOutput, error) {
	outcome, err := projectsOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return worktile.ListProjectsOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input worktile.ListProjectsInput) worktile.ListProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListProjects" }
func (startRequest) WaitFor(dex.Context, worktile.ListProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input worktile.ListProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[worktile.ListProjectsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[worktile.ListProjectsResult]
}

func (completeResult) GetStepType() string { return "CompleteListProjects" }
func (completeResult) Execute(ctx dex.Context, result worktile.ListProjectsResult) (*dex.StepDecision, error) {
	if err := projectsOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
