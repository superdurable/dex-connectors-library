// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const projectListingStepType = "CallListProjects"

var projectListingAttribute = dex.DefineAttribute[meistertask.ProjectsPage]("meistertask-project-list")

// ProjectListingFlow reads one page of projects for a Dex application.
type ProjectListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewProjectListingFlow binds the same static MeisterTask connection used by project inspection.
func NewProjectListingFlow(connection meistertask.Connection) *ProjectListingFlow {
	return &ProjectListingFlow{connection: connection}
}

// GetFlowType identifies the project listing Flow.
func (*ProjectListingFlow) GetFlowType() string { return "MeisterTaskProjectListing" }

// GetPersistenceSchema registers its durable provider result.
func (*ProjectListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectListingAttribute}}
}

// GetSteps wires the project listing Query through an operation-specific factory.
func (flow *ProjectListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectListing{}),
		dex.DefineStep(meistertask.NewListProjectsStep(meistertask.ListProjectsStepConfig[meistertask.ListProjectsInput]{
			StepType: projectListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List projects visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listProjectsInput,
			Listed: sdkgo.GoTo(completeProjectListing{}),
		})),
		dex.DefineStep(completeProjectListing{}),
	}
}

// GetRPCs exposes the project page in Dex Web.
func (flow *ProjectListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-project-list value-type:json editable:false description:"Project page"
// GetDexSummary returns the last project page.
func (*ProjectListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	projects, err := readProjectListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-project-list": projects}}, nil
}

// dex:field attribute-key:meistertask-project-list value-type:json editable:false description:"Project page"
// GetDexDisplay returns the last project page.
func (*ProjectListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	projects, err := readProjectListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-project-list": projects}}, nil
}

func readProjectListing(ctx dex.Context) (meistertask.ProjectsPage, error) {
	projects, err := projectListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.ProjectsPage{}, nil
	}
	return projects, err
}

func listProjectsInput(input meistertask.ListProjectsInput) meistertask.ListProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startProjectListing struct{ dex.StepDefaults }

func (startProjectListing) GetStepType() string { return "StartProjectListing" }
func (startProjectListing) WaitFor(dex.Context, meistertask.ListProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectListing) Execute(_ dex.Context, input meistertask.ListProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListProjectsInput](projectListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned project page and complete the Flow."
type completeProjectListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListProjectsResult]
}

func (completeProjectListing) GetStepType() string { return "CompleteProjectListing" }
func (completeProjectListing) Execute(ctx dex.Context, result meistertask.ListProjectsResult) (*dex.StepDecision, error) {
	if err := projectListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectListingFlow)(nil)
