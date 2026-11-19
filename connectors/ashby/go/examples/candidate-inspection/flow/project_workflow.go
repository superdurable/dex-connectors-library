// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const projectOperationStepType = "CallAddCandidateProject"

var projectCandidateAttribute = dex.DefineAttribute[ashby.CandidateProfile]("ashby-project-candidate")

// CandidateProjectFlow associates an existing project with a candidate.
type CandidateProjectFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateProjectFlow binds the provider connection at registration time.
func NewCandidateProjectFlow(connection ashby.Connection) *CandidateProjectFlow {
	return &CandidateProjectFlow{connection: connection}
}

// GetFlowType returns the stable project example identity.
func (*CandidateProjectFlow) GetFlowType() string { return "AshbyCandidateProject" }

// GetPersistenceSchema registers the retained candidate result.
func (*CandidateProjectFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{projectCandidateAttribute}}
}

// GetSteps registers the start, project Mutation, and completion Steps.
func (flow *CandidateProjectFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(projectStartRequest{}),
		dex.DefineStep(ashby.NewAddCandidateProjectStep(ashby.AddCandidateProjectStepConfig[ashby.CandidateProjectInput]{
			StepType: projectOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Associate an existing project with a candidate in Ashby."},
			Connection: flow.connection, MapToOperationInput: mapToCandidateProjectInput,
			Added: sdkgo.GoTo(completeProjectResult{}),
		})),
		dex.DefineStep(completeProjectResult{}),
	}
}

// GetRPCs exposes the read-only Dex Web views.
func (flow *CandidateProjectFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-project-candidate value-type:json editable:false description:"Project candidate"
// GetDexSummary shows the project candidate in Dex Web.
func (*CandidateProjectFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readProjectCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-project-candidate": profile}}, nil
}

// dex:field attribute-key:ashby-project-candidate value-type:json editable:false description:"Project candidate"
// GetDexDisplay shows the project candidate in Dex Web.
func (*CandidateProjectFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readProjectCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-project-candidate": profile}}, nil
}

func readProjectCandidate(ctx dex.Context) (ashby.CandidateProfile, error) {
	profile, err := projectCandidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateProfile{}, nil
	}
	return profile, err
}

func mapToCandidateProjectInput(input ashby.CandidateProjectInput) ashby.CandidateProjectInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate and project IDs to the association Step."
type projectStartRequest struct{ dex.StepDefaults }

func (projectStartRequest) GetStepType() string { return "StartAddCandidateProject" }
func (projectStartRequest) WaitFor(dex.Context, ashby.CandidateProjectInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (projectStartRequest) Execute(_ dex.Context, input ashby.CandidateProjectInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CandidateProjectInput](projectOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the project candidate and complete the Flow."
type completeProjectResult struct{ dex.StepDefaultsNoWaitFor[ashby.AddCandidateProjectResult] }

func (completeProjectResult) GetStepType() string { return "CompleteAddCandidateProject" }
func (completeProjectResult) Execute(ctx dex.Context, result ashby.AddCandidateProjectResult) (*dex.StepDecision, error) {
	if err := projectCandidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateProjectFlow)(nil)
