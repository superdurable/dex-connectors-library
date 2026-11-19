// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createOperationStepType = "CallCreateCandidate"

var createdCandidateAttribute = dex.DefineAttribute[ashby.CandidateProfile]("ashby-created-candidate")

// CandidateCreateFlow creates a candidate.
type CandidateCreateFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateCreateFlow binds the provider connection at registration time.
func NewCandidateCreateFlow(connection ashby.Connection) *CandidateCreateFlow {
	return &CandidateCreateFlow{connection: connection}
}

// GetFlowType returns the stable create example identity.
func (*CandidateCreateFlow) GetFlowType() string { return "AshbyCandidateCreate" }

// GetPersistenceSchema registers the retained candidate result.
func (*CandidateCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdCandidateAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *CandidateCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(createStartRequest{}),
		dex.DefineStep(ashby.NewCreateCandidateStep(ashby.CreateCandidateStepConfig[ashby.CreateCandidateInput]{
			StepType: createOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Create a candidate in Ashby."},
			Connection: flow.connection, MapToOperationInput: mapToCreateCandidateInput,
			Created: sdkgo.GoTo(completeCreateResult{}),
		})),
		dex.DefineStep(completeCreateResult{}),
	}
}

// GetRPCs exposes the read-only Dex Web views.
func (flow *CandidateCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-created-candidate value-type:json editable:false description:"Created candidate"
// GetDexSummary shows the created candidate in Dex Web.
func (*CandidateCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCreatedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-candidate": profile}}, nil
}

// dex:field attribute-key:ashby-created-candidate value-type:json editable:false description:"Created candidate"
// GetDexDisplay shows the created candidate in Dex Web.
func (*CandidateCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCreatedCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-candidate": profile}}, nil
}

func readCreatedCandidate(ctx dex.Context) (ashby.CandidateProfile, error) {
	profile, err := createdCandidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateProfile{}, nil
	}
	return profile, err
}

func mapToCreateCandidateInput(input ashby.CreateCandidateInput) ashby.CreateCandidateInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate name to the create Step."
type createStartRequest struct{ dex.StepDefaults }

func (createStartRequest) GetStepType() string { return "StartCreateCandidate" }
func (createStartRequest) WaitFor(dex.Context, ashby.CreateCandidateInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (createStartRequest) Execute(_ dex.Context, input ashby.CreateCandidateInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CreateCandidateInput](createOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created candidate and complete the Flow."
type completeCreateResult struct{ dex.StepDefaultsNoWaitFor[ashby.CreateCandidateResult] }

func (completeCreateResult) GetStepType() string { return "CompleteCreateCandidate" }
func (completeCreateResult) Execute(ctx dex.Context, result ashby.CreateCandidateResult) (*dex.StepDecision, error) {
	if err := createdCandidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateCreateFlow)(nil)
