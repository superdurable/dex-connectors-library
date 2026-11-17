// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accounts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/basecamp/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProjectCreateFlowType identifies the project creation example.
const ProjectCreateFlowType = "BasecampProjectCreate"

const projectCreateStepType = "CallCreateProject"

var createdProjectAttribute = dex.DefineAttribute[basecamp.Project]("basecamp-created-project")

// ProjectCreateFlow creates one Basecamp project.
type ProjectCreateFlow struct {
	dex.FlowDefaults
	connection basecamp.Connection
}

// NewProjectCreateFlow binds the Basecamp connection at registration.
func NewProjectCreateFlow(connection basecamp.Connection) *ProjectCreateFlow {
	return &ProjectCreateFlow{connection: connection}
}

func (*ProjectCreateFlow) GetFlowType() string { return ProjectCreateFlowType }

func (*ProjectCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdProjectAttribute}}
}

// GetRPCs registers the read-only project views.
func (flow *ProjectCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:basecamp-created-project value-type:json editable:false description:"Created project"
// GetDexSummary shows the created project identity.
func (*ProjectCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	project, err := readCreatedProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-project": project}}, nil
}

// dex:field attribute-key:basecamp-created-project value-type:json editable:false description:"Created project"
// GetDexDisplay shows the created project identity.
func (*ProjectCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	project, err := readCreatedProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"basecamp-created-project": project}}, nil
}

func readCreatedProject(ctx dex.Context) (basecamp.Project, error) {
	project, err := createdProjectAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return basecamp.Project{}, nil
	}
	return project, err
}

func (flow *ProjectCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectCreate{}),
		dex.DefineStep(basecamp.NewCreateProjectStep(basecamp.CreateProjectStepConfig[basecamp.CreateProjectInput]{
			StepType: projectCreateStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Basecamp", Explanation: "Create one named project in the selected account."},
			Connection:          flow.connection,
			MapToOperationInput: func(input basecamp.CreateProjectInput) basecamp.CreateProjectInput { return input },
			Created:             sdkgo.GoTo(completeProjectCreate{}),
		})),
		dex.DefineStep(completeProjectCreate{}),
	}
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the project fields to the creation Step."
type startProjectCreate struct{ dex.StepDefaults }

func (startProjectCreate) GetStepType() string { return "StartCreateProject" }
func (startProjectCreate) WaitFor(dex.Context, basecamp.CreateProjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectCreate) Execute(_ dex.Context, input basecamp.CreateProjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[basecamp.CreateProjectInput](projectCreateStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the created project identity."
type completeProjectCreate struct {
	dex.StepDefaultsNoWaitFor[basecamp.CreateProjectResult]
}

func (completeProjectCreate) GetStepType() string { return "CompleteCreateProject" }
func (completeProjectCreate) Execute(ctx dex.Context, result basecamp.CreateProjectResult) (*dex.StepDecision, error) {
	if err := createdProjectAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectCreateFlow)(nil)
