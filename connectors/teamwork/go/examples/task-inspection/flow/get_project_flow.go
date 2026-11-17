// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const getProjectStepType = "CallGetProject"

var projectAttribute = dex.DefineAttribute[teamwork.ProjectSummary]("teamwork-project")

// GetProjectFlow reads one Teamwork project through the static connection.
type GetProjectFlow struct {
	dex.FlowDefaults
	connection teamwork.Connection
}

// NewGetProjectFlow binds the Teamwork connection at Worker registration.
func NewGetProjectFlow(connection teamwork.Connection) *GetProjectFlow {
	return &GetProjectFlow{connection: connection}
}

// GetFlowType returns the stable project inspection Flow identity.
func (*GetProjectFlow) GetFlowType() string { return "TeamworkGetProject" }

// GetPersistenceSchema registers project metadata.
func (*GetProjectFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectAttribute}}
}

// GetSteps registers the project query and completion Steps.
func (flow *GetProjectFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(beginGetProject{}),
		dex.DefineStep(teamwork.NewGetProjectStep(teamwork.GetProjectStepConfig[teamwork.GetProjectInput]{
			StepType: getProjectStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Teamwork", Explanation: "Read a project's ID, name, and status."},
			MapToOperationInput: mapGetProjectInput, Found: sdkgo.GoTo(completeGetProject{}),
		})),
		dex.DefineStep(completeGetProject{}),
	}
}

// GetRPCs exposes read-only Dex Web project metadata.
func (flow *GetProjectFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns project metadata when available.
// dex:field attribute-key:teamwork-project value-type:json editable:false description:"Project metadata"
func (*GetProjectFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	project, err := readProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-project": project}}, nil
}

// GetDexDisplay returns project metadata when available.
// dex:field attribute-key:teamwork-project value-type:json editable:false description:"Project metadata"
func (*GetProjectFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	project, err := readProject(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"teamwork-project": project}}, nil
}

func readProject(ctx dex.Context) (teamwork.ProjectSummary, error) {
	project, err := projectAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return teamwork.ProjectSummary{}, nil
	}
	return project, err
}

func mapGetProjectInput(input teamwork.GetProjectInput) teamwork.GetProjectInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the project ID to the provider Step."
type beginGetProject struct{ dex.StepDefaults }

func (beginGetProject) WaitFor(dex.Context, teamwork.GetProjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (beginGetProject) Execute(_ dex.Context, input teamwork.GetProjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[teamwork.GetProjectInput](getProjectStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with project metadata."
type completeGetProject struct {
	dex.StepDefaultsNoWaitFor[teamwork.GetProjectResult]
}

func (completeGetProject) Execute(ctx dex.Context, result teamwork.GetProjectResult) (*dex.StepDecision, error) {
	if err := projectAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetProjectFlow)(nil)
