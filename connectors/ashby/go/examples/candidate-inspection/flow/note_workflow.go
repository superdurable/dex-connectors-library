// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const noteOperationStepType = "CallCreateCandidateNote"

var createdNoteAttribute = dex.DefineAttribute[ashby.CandidateNoteSummary]("ashby-created-note")

// CandidateNoteFlow creates a candidate note.
type CandidateNoteFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateNoteFlow binds the provider connection at registration time.
func NewCandidateNoteFlow(connection ashby.Connection) *CandidateNoteFlow {
	return &CandidateNoteFlow{connection: connection}
}

// GetFlowType returns the stable create example identity.
func (*CandidateNoteFlow) GetFlowType() string { return "AshbyCandidateNote" }

// GetPersistenceSchema registers the retained candidate result.
func (*CandidateNoteFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdNoteAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *CandidateNoteFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(noteStartRequest{}),
		dex.DefineStep(ashby.NewCreateCandidateNoteStep(ashby.CreateCandidateNoteStepConfig[ashby.CreateCandidateNoteInput]{
			StepType: noteOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Create a plaintext note on a candidate in Ashby."},
			Connection: flow.connection, MapToOperationInput: mapToCreateCandidateNoteInput,
			Created: sdkgo.GoTo(completeNoteResult{}),
		})),
		dex.DefineStep(completeNoteResult{}),
	}
}

// GetRPCs exposes the read-only Dex Web views.
func (flow *CandidateNoteFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-created-note value-type:json editable:false description:"Created note"
// GetDexSummary shows the created note in Dex Web.
func (*CandidateNoteFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCreatedNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-note": profile}}, nil
}

// dex:field attribute-key:ashby-created-note value-type:json editable:false description:"Created note"
// GetDexDisplay shows the created note in Dex Web.
func (*CandidateNoteFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCreatedNote(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-created-note": profile}}, nil
}

func readCreatedNote(ctx dex.Context) (ashby.CandidateNoteSummary, error) {
	profile, err := createdNoteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateNoteSummary{}, nil
	}
	return profile, err
}

func mapToCreateCandidateNoteInput(input ashby.CreateCandidateNoteInput) ashby.CreateCandidateNoteInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the candidate ID and note to the create Step."
type noteStartRequest struct{ dex.StepDefaults }

func (noteStartRequest) GetStepType() string { return "StartCreateCandidateNote" }
func (noteStartRequest) WaitFor(dex.Context, ashby.CreateCandidateNoteInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (noteStartRequest) Execute(_ dex.Context, input ashby.CreateCandidateNoteInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.CreateCandidateNoteInput](noteOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the created note and complete the Flow."
type completeNoteResult struct{ dex.StepDefaultsNoWaitFor[ashby.CreateCandidateNoteResult] }

func (completeNoteResult) GetStepType() string { return "CompleteCreateCandidateNote" }
func (completeNoteResult) Execute(ctx dex.Context, result ashby.CreateCandidateNoteResult) (*dex.StepDecision, error) {
	if err := createdNoteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateNoteFlow)(nil)
