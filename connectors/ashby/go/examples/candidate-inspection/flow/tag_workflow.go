// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const tagOperationStepType = "CallAddCandidateTag"

var taggedCandidateAttribute = dex.DefineAttribute[ashby.CandidateProfile]("ashby-tagged-candidate")

// CandidateTagFlow associates an existing tag with a candidate.
type CandidateTagFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateTagFlow binds the provider connection at registration time.
func NewCandidateTagFlow(connection ashby.Connection) *CandidateTagFlow {
	return &CandidateTagFlow{connection: connection}
}

// GetFlowType returns the stable tag example identity.
func (*CandidateTagFlow) GetFlowType() string { return "AshbyCandidateTag" }

// GetPersistenceSchema registers the retained candidate result.
func (*CandidateTagFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{taggedCandidateAttribute}}
}

// GetSteps registers the start, tag Mutation, and completion Steps.
func (flow *CandidateTagFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(tagStartRequest{}),
		dex.DefineStep(ashby.NewAddCandidateTagStep(ashby.AddCandidateTagStepConfig[ashby.CandidateTagInput]{
			StepType: tagOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Associate an existing tag with a candidate in Ashby."},
			Connection: flow.connection, MapToOperationInput: mapToCandidateTagInput,
			Added: sdkgo.GoTo(completeTagResult{}),
		})),
		dex.DefineStep(completeTagResult{}),
	}
}

// GetRPCs exposes the read-only Dex Web views.
func (flow *CandidateTagFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-tagged-candidate value-type:json editable:false description:"Tagged candidate"
// GetDexSummary shows the tagged candidate in Dex Web.
func (*CandidateTagFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readTaggedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-tagged-candidate": profile}}, nil
}

// dex:field attribute-key:ashby-tagged-candidate value-type:json editable:false description:"Tagged candidate"
// GetDexDisplay shows the tagged candidate in Dex Web.
func (*CandidateTagFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readTaggedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-tagged-candidate": profile}}, nil
}

func readTaggedCandidate(ctx dex.Context) (ashby.CandidateProfile, error) {
	profile, err := taggedCandidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateProfile{}, nil
	}
	return profile, err
}

func mapToCandidateTagInput(input ashby.CandidateTagInput) ashby.CandidateTagInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate and tag IDs to the association Step."
type tagStartRequest struct{ dex.StepDefaults }

func (tagStartRequest) GetStepType() string { return "StartAddCandidateTag" }
func (tagStartRequest) WaitFor(dex.Context, ashby.CandidateTagInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (tagStartRequest) Execute(_ dex.Context, input ashby.CandidateTagInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CandidateTagInput](tagOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the tagged candidate and complete the Flow."
type completeTagResult struct{ dex.StepDefaultsNoWaitFor[ashby.AddCandidateTagResult] }

func (completeTagResult) GetStepType() string { return "CompleteAddCandidateTag" }
func (completeTagResult) Execute(ctx dex.Context, result ashby.AddCandidateTagResult) (*dex.StepDecision, error) {
	if err := taggedCandidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateTagFlow)(nil)
