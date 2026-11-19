// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const renameOperationStepType = "CallUpdateCandidate"

var renamedCandidateAttribute = dex.DefineAttribute[ashby.CandidateProfile]("ashby-renamed-candidate")

// CandidateRenameFlow replaces a candidate display name through an Ashby Mutation Step.
type CandidateRenameFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateRenameFlow binds the provider connection at registration time.
func NewCandidateRenameFlow(connection ashby.Connection) *CandidateRenameFlow {
	return &CandidateRenameFlow{connection: connection}
}

// GetFlowType returns the stable rename example identity.
func (*CandidateRenameFlow) GetFlowType() string { return "AshbyCandidateRename" }

// GetPersistenceSchema registers the result retained after completion.
func (*CandidateRenameFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{renamedCandidateAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *CandidateRenameFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(renameStartRequest{}),
		dex.DefineStep(ashby.NewUpdateCandidateStep(ashby.UpdateCandidateStepConfig[ashby.UpdateCandidateInput]{
			StepType: renameOperationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Replace a candidate display name in Ashby."},
			Connection:          flow.connection,
			MapToOperationInput: mapToUpdateCandidateInput,
			Updated:             sdkgo.GoTo(completeRenameResult{}),
		})),
		dex.DefineStep(completeRenameResult{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the retained result.
func (flow *CandidateRenameFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-renamed-candidate value-type:json editable:false description:"Updated candidate"
// GetDexSummary shows the updated candidate in Dex Web.
func (*CandidateRenameFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readRenamedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-renamed-candidate": profile}}, nil
}

// dex:field attribute-key:ashby-renamed-candidate value-type:json editable:false description:"Updated candidate"
// GetDexDisplay shows the updated candidate in Dex Web.
func (*CandidateRenameFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readRenamedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-renamed-candidate": profile}}, nil
}

func readRenamedCandidate(ctx dex.Context) (ashby.CandidateProfile, error) {
	profile, err := renamedCandidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateProfile{}, nil
	}
	return profile, err
}

func mapToUpdateCandidateInput(input ashby.UpdateCandidateInput) ashby.UpdateCandidateInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate ID and new name to the update Step."
type renameStartRequest struct{ dex.StepDefaults }

func (renameStartRequest) GetStepType() string { return "StartUpdateCandidate" }
func (renameStartRequest) WaitFor(dex.Context, ashby.UpdateCandidateInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (renameStartRequest) Execute(_ dex.Context, input ashby.UpdateCandidateInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.UpdateCandidateInput](renameOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the updated candidate and complete the rename Flow."
type completeRenameResult struct {
	dex.StepDefaultsNoWaitFor[ashby.UpdateCandidateResult]
}

func (completeRenameResult) GetStepType() string { return "CompleteUpdateCandidate" }
func (completeRenameResult) Execute(ctx dex.Context, result ashby.UpdateCandidateResult) (*dex.StepDecision, error) {
	if err := renamedCandidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateRenameFlow)(nil)
