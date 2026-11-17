// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const listProjectsStepType = "ListCopperProjects"

var projectsPageAttribute = dex.DefineAttribute[copper.ProjectsPage]("copper-projects-page")

// ProjectsSearchFlow reads one bounded page of Copper projects.
type ProjectsSearchFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewProjectsSearchFlow binds the static Copper connection.
func NewProjectsSearchFlow(connection copper.Connection) *ProjectsSearchFlow {
	return &ProjectsSearchFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ProjectsSearchFlow) GetFlowType() string { return "CopperProjectSearch" }

// GetPersistenceSchema retains the search result for Dex Web.
func (*ProjectsSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectsPageAttribute}}
}

// GetSteps wires the operation-specific projects Query.
func (flow *ProjectsSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectsSearch{}),
		dex.DefineStep(copper.NewListProjectsStep(copper.ListProjectsStepConfig[copper.ListProjectsInput]{
			StepType: listProjectsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Read one page of Copper projects."},
			Connection:  flow.connection, MapToOperationInput: mapProjectsSearchInput,
			Listed: sdkgo.GoTo(completeProjectsSearch{}),
		})),
		dex.DefineStep(completeProjectsSearch{}),
	}
}

// GetRPCs exposes read-only Dex Web search views.
func (flow *ProjectsSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-projects-page value-type:json editable:false description:"Projects search page"
// GetDexSummary shows the returned page in Dex Web.
func (*ProjectsSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProjectsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-projects-page": page}}, nil
}

// dex:field attribute-key:copper-projects-page value-type:json editable:false description:"Projects search page"
// GetDexDisplay shows the returned page in Dex Web.
func (*ProjectsSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProjectsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-projects-page": page}}, nil
}

func readProjectsPage(ctx dex.Context) (copper.ProjectsPage, error) {
	page, err := projectsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.ProjectsPage{}, nil
	}
	return page, err
}

func mapProjectsSearchInput(input copper.ListProjectsInput) copper.ListProjectsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested projects page and filters to Copper."
type startProjectsSearch struct{ dex.StepDefaults }

func (startProjectsSearch) WaitFor(dex.Context, copper.ListProjectsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectsSearch) Execute(_ dex.Context, input copper.ListProjectsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.ListProjectsInput](listProjectsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the projects page and complete the Flow."
type completeProjectsSearch struct {
	dex.StepDefaultsNoWaitFor[copper.ListProjectsResult]
}

func (completeProjectsSearch) Execute(ctx dex.Context, result copper.ListProjectsResult) (*dex.StepDecision, error) {
	if err := projectsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectsSearchFlow)(nil)
