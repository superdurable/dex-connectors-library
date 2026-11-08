// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package candidateinspection runs Ashby getCandidate as one Dex Flow.
package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "AshbyCandidateInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "ashby"
	operationStepType = "CallGetCandidate"
)

var candidateAttribute = dex.DefineAttribute[ashby.CandidateProfile]("ashby-candidate")

// Flow runs GetCandidate once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection ashby.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{candidateAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(ashby.NewGetCandidateStep(ashby.GetCandidateStepConfig[ashby.GetCandidateInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Read a candidate from Ashby."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-candidate value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-candidate": outcome}}, nil
}

// dex:field attribute-key:ashby-candidate value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-candidate": outcome}}, nil
}

func readCandidate(ctx dex.Context) (ashby.CandidateProfile, error) {
	outcome, err := candidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input ashby.GetCandidateInput) ashby.GetCandidateInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCandidate" }
func (startRequest) WaitFor(dex.Context, ashby.GetCandidateInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input ashby.GetCandidateInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.GetCandidateInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[ashby.GetCandidateResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCandidate" }
func (completeResult) Execute(ctx dex.Context, result ashby.GetCandidateResult) (*dex.StepDecision, error) {
	if err := candidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
