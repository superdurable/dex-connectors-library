// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercellookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/vercel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ProjectDomainFlowType identifies the create-project-and-add-domain example.
const ProjectDomainFlowType = "VercelCreateProjectWithDomain"

var (
	projectDomainRequestAttribute = dex.DefineAttribute[ProjectDomainRequest]("vercel-project-domain-request")
	projectDomainOutputAttribute  = dex.DefineAttribute[vercel.ProjectDomain]("vercel-project-domain-output")
)

// ProjectDomainRequest supplies a project name, domain, and optional team.
type ProjectDomainRequest struct {
	// Name is the new Vercel project name.
	Name string `json:"name"`
	// Domain is the domain to assign to the new project.
	Domain string `json:"domain"`
	// TeamID selects a team; blank selects the personal account.
	TeamID string `json:"teamId,omitempty"`
}

// ProjectDomainFlow creates a project and assigns its domain.
type ProjectDomainFlow struct {
	dex.FlowDefaults
	connection vercel.Connection
}

// NewProjectDomainFlow binds the static Vercel connection.
func NewProjectDomainFlow(connection vercel.Connection) *ProjectDomainFlow {
	return &ProjectDomainFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*ProjectDomainFlow) GetFlowType() string { return ProjectDomainFlowType }

// GetPersistenceSchema registers the request and result Attributes.
func (*ProjectDomainFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectDomainRequestAttribute, projectDomainOutputAttribute}}
}

// GetSteps registers project creation, domain assignment, and completion.
func (flow *ProjectDomainFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectDomain{}),
		dex.DefineStep(vercel.NewCreateProjectStep(vercel.CreateProjectStepConfig[ProjectDomainRequest]{
			StepType: "CallCreateProject", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "vercel", GroupLabel: "Vercel", Explanation: "Create the named Vercel project."},
			MapToOperationInput: mapProjectDomainToProject, Created: sdkgo.GoTo(prepareProjectDomain{}),
		})),
		dex.DefineStep(prepareProjectDomain{}),
		dex.DefineStep(vercel.NewAddProjectDomainStep(vercel.AddProjectDomainStepConfig[vercel.ProjectDomainInput]{
			StepType: "CallAddProjectDomain", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "vercel", GroupLabel: "Vercel", Explanation: "Assign the requested domain to the new project."},
			MapToOperationInput: mapProjectDomainInput, Added: sdkgo.GoTo(completeProjectDomain{}),
		})),
		dex.DefineStep(completeProjectDomain{}),
	}
}

// GetRPCs registers read-only summary and display RPCs.
func (flow *ProjectDomainFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:vercel-project-domain-request value-type:json editable:false description:"Project and domain request"
// dex:field attribute-key:vercel-project-domain-output value-type:json editable:false description:"Assigned domain"
// GetDexSummary returns the request and last assigned domain.
func (*ProjectDomainFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := optionalAttribute(ctx, projectDomainRequestAttribute)
	if err != nil {
		return nil, err
	}
	domain, err := optionalAttribute(ctx, projectDomainOutputAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vercel-project-domain-request": request, "vercel-project-domain-output": domain}}, nil
}

// dex:field attribute-key:vercel-project-domain-request value-type:json editable:false description:"Project and domain request"
// dex:field attribute-key:vercel-project-domain-output value-type:json editable:false description:"Assigned domain"
// GetDexDisplay returns the request and last assigned domain.
func (*ProjectDomainFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, err := optionalAttribute(ctx, projectDomainRequestAttribute)
	if err != nil {
		return nil, err
	}
	domain, err := optionalAttribute(ctx, projectDomainOutputAttribute)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"vercel-project-domain-request": request, "vercel-project-domain-output": domain}}, nil
}

func mapProjectDomainToProject(input ProjectDomainRequest) vercel.CreateProjectInput {
	return vercel.CreateProjectInput{Name: input.Name, TeamID: input.TeamID}
}
func mapProjectDomainInput(input vercel.ProjectDomainInput) vercel.ProjectDomainInput { return input }

// dex:group group-id:vercel group-label:"Vercel"
// dex:explanation text:"Save the domain request before creating the project."
type startProjectDomain struct{ dex.StepDefaults }

func (startProjectDomain) GetStepType() string { return "StartVercelProjectDomain" }
func (startProjectDomain) WaitFor(dex.Context, ProjectDomainRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectDomain) Execute(ctx dex.Context, input ProjectDomainRequest) (*dex.StepDecision, error) {
	if err := projectDomainRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ProjectDomainRequest]("CallCreateProject"), input), nil
}

// dex:group group-id:vercel group-label:"Vercel"
// dex:explanation text:"Use the newly created project ID when assigning its domain."
type prepareProjectDomain struct {
	dex.StepDefaultsNoWaitFor[vercel.CreateProjectResult]
}

func (prepareProjectDomain) GetStepType() string { return "PrepareVercelProjectDomain" }
func (prepareProjectDomain) Execute(ctx dex.Context, result vercel.CreateProjectResult) (*dex.StepDecision, error) {
	request, err := projectDomainRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[vercel.ProjectDomainInput]("CallAddProjectDomain"), vercel.ProjectDomainInput{ProjectID: result.Value.ID, Domain: request.Domain, TeamID: request.TeamID}), nil
}

// dex:group group-id:vercel group-label:"Vercel"
// dex:explanation text:"Complete with the assigned domain metadata."
type completeProjectDomain struct {
	dex.StepDefaultsNoWaitFor[vercel.AddProjectDomainResult]
}

func (completeProjectDomain) GetStepType() string { return "CompleteVercelProjectDomain" }
func (completeProjectDomain) Execute(ctx dex.Context, result vercel.AddProjectDomainResult) (*dex.StepDecision, error) {
	if err := projectDomainOutputAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectDomainFlow)(nil)
