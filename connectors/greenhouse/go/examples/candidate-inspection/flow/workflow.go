// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package candidateinspection runs Greenhouse getCandidate as one Dex Flow.
package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/greenhouse/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GreenhouseCandidateInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "greenhouse"
	operationStepType = "CallGetCandidate"
)

var candidateAttribute = dex.DefineAttribute[greenhouse.CandidateProfile]("greenhouse-candidate")

// Flow runs GetCandidate once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection greenhouse.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection greenhouse.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{candidateAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(greenhouse.NewGetCandidateStep(greenhouse.GetCandidateStepConfig[greenhouse.GetCandidateInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Greenhouse", Explanation: "Read a candidate's name from Greenhouse."},
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

// dex:field attribute-key:greenhouse-candidate value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"greenhouse-candidate": outcome}}, nil
}

// dex:field attribute-key:greenhouse-candidate value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readCandidate(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"greenhouse-candidate": outcome}}, nil
}

func readCandidate(ctx dex.Context) (greenhouse.CandidateProfile, error) {
	outcome, err := candidateAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return greenhouse.CandidateProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input greenhouse.GetCandidateInput) greenhouse.GetCandidateInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetCandidate" }
func (startRequest) WaitFor(dex.Context, greenhouse.GetCandidateInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input greenhouse.GetCandidateInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[greenhouse.GetCandidateInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[greenhouse.GetCandidateResult]
}

func (completeResult) GetStepType() string { return "CompleteGetCandidate" }
func (completeResult) Execute(ctx dex.Context, result greenhouse.GetCandidateResult) (*dex.StepDecision, error) {
	if err := candidateAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
