// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProjectsFlowType identifies the project listing example.
const ProjectsFlowType = "BasecampProjectsList"

const projectsOperationStepType = "CallListProjects"

var projectsAttribute = dex.DefineAttribute[basecamp.ProjectsPage]("basecamp-projects")

// ProjectsFlow lists the active projects for one Basecamp account.
type ProjectsFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewProjectsFlow binds the provider connection at registration time.
func NewProjectsFlow(connection basecamp.Connection) *ProjectsFlow {
	return &ProjectsFlow{connection: connection}
}

func (*ProjectsFlow) GetFlowType() string { return ProjectsFlowType }

func (*ProjectsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectsAttribute}}
}

func (flow *ProjectsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectsRequest{}),
		dex.DefineStep(basecamp.NewListProjectsStep(basecamp.ListProjectsStepConfig[basecamp.ListProjectsInput]{
			StepType: projectsOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "List active projects in the selected Basecamp account."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.ListProjectsInput) basecamp.ListProjectsInput { return input },
			Listed:              sdkgo.GoTo(completeProjectsResult{}),
		})),
		dex.DefineStep(completeProjectsResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *ProjectsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-projects value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last project page.
func (*ProjectsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	projects, err := readProjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-projects": projects}}, nil
}

// dex:field attribute-key:basecamp-projects value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last project page.
func (*ProjectsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	projects, err := readProjects(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-projects": projects}}, nil
}

func readProjects(ctx dex.Context) (basecamp.ProjectsPage, error) {
	projects, err := projectsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.ProjectsPage{}, nil
	}
	return projects, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account ID to the provider Step."
type startProjectsRequest struct{ dex.StepDefaults }

func (startProjectsRequest) GetStepType() string { return "StartListProjects" }
func (startProjectsRequest) WaitFor(dex.Context, basecamp.ListProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectsRequest) Execute(_ dex.Context, input basecamp.ListProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.ListProjectsInput](projectsOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the project page."
type completeProjectsResult struct {
	dex.StepDefaultsNoWaitFor[basecamp.ListProjectsResult]
}

func (completeProjectsResult) GetStepType() string { return "CompleteListProjects" }
func (completeProjectsResult) Execute(ctx dex.Context, result basecamp.ListProjectsResult) (*dex.StepDecision, error) {
	if err := projectsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectsFlow)(nil)
