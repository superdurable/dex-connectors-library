// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package personlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/copper/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createProjectStepType = "CreateCopperProject"

var createdProjectAttribute = dex.DefineAttribute[copper.ProjectMetadata]("copper-created-project")

// ProjectCreationFlow creates one Copper project with bounded sends.
type ProjectCreationFlow struct {
	dex.FlowDefaults
	connection copper.Connection
}

// NewProjectCreationFlow binds the static Copper connection.
func NewProjectCreationFlow(connection copper.Connection) *ProjectCreationFlow {
	return &ProjectCreationFlow{connection: connection}
}

// GetFlowType gives this example a stable Flow identity.
func (*ProjectCreationFlow) GetFlowType() string { return "CopperProjectCreation" }

// GetPersistenceSchema retains the created project for Dex Web.
func (*ProjectCreationFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdProjectAttribute}}
}

// GetSteps wires the operation-specific project Mutation.
func (flow *ProjectCreationFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startProjectCreation{}),
		dex.DefineStep(copper.NewCreateProjectStep(copper.CreateProjectStepConfig[copper.CreateProjectInput]{
			StepType: createProjectStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Copper", Explanation: "Create a Copper project."},
			Connection:  flow.connection, MapToOperationInput: mapProjectCreationInput,
			Created: sdkgo.GoTo(completeProjectCreation{}),
		})),
		dex.DefineStep(completeProjectCreation{}),
	}
}

// GetRPCs exposes read-only Dex Web result views.
func (flow *ProjectCreationFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:copper-created-project value-type:json editable:false description:"Created project"
// GetDexSummary shows the created project in Dex Web.
func (*ProjectCreationFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProjectMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-project": page}}, nil
}

// dex:field attribute-key:copper-created-project value-type:json editable:false description:"Created project"
// GetDexDisplay shows the created project in Dex Web.
func (*ProjectCreationFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readProjectMetadata(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"copper-created-project": page}}, nil
}

func readProjectMetadata(ctx dex.Context) (copper.ProjectMetadata, error) {
	page, err := createdProjectAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return copper.ProjectMetadata{}, nil
	}
	return page, err
}

func mapProjectCreationInput(input copper.CreateProjectInput) copper.CreateProjectInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested project name to Copper."
type startProjectCreation struct{ dex.StepDefaults }

func (startProjectCreation) WaitFor(dex.Context, copper.CreateProjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startProjectCreation) Execute(_ dex.Context, input copper.CreateProjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[copper.CreateProjectInput](createProjectStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created project and complete the Flow."
type completeProjectCreation struct {
	dex.StepDefaultsNoWaitFor[copper.CreateProjectResult]
}

func (completeProjectCreation) Execute(ctx dex.Context, result copper.CreateProjectResult) (*dex.StepDecision, error) {
	if err := createdProjectAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ProjectCreationFlow)(nil)
