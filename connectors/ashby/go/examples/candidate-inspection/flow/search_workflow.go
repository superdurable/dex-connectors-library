// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const searchOperationStepType = "CallSearchCandidates"

var searchedCandidatesAttribute = dex.DefineAttribute[ashby.CandidateSearchResults]("ashby-searched-candidates")

// CandidateSearchFlow searches candidate identities.
type CandidateSearchFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewCandidateSearchFlow binds the provider connection at registration time.
func NewCandidateSearchFlow(connection ashby.Connection) *CandidateSearchFlow {
	return &CandidateSearchFlow{connection: connection}
}

// GetFlowType returns the stable search example identity.
func (*CandidateSearchFlow) GetFlowType() string { return "AshbyCandidateSearch" }

// GetPersistenceSchema registers the retained candidate result.
func (*CandidateSearchFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{searchedCandidatesAttribute}}
}

// GetSteps registers the start, search Query, and completion Steps.
func (flow *CandidateSearchFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(searchStartRequest{}),
		dex.DefineStep(ashby.NewSearchCandidatesStep(ashby.SearchCandidatesStepConfig[ashby.SearchCandidatesInput]{
			StepType: searchOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Search candidate identities in Ashby."},
			Connection: flow.connection, MapToOperationInput: mapToSearchCandidatesInput,
			Found: sdkgo.GoTo(completeSearchResult{}),
		})),
		dex.DefineStep(completeSearchResult{}),
	}
}

// GetRPCs exposes the read-only Dex Web views.
func (flow *CandidateSearchFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-searched-candidates value-type:json editable:false description:"Searched candidates"
// GetDexSummary shows the searched candidates in Dex Web.
func (*CandidateSearchFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readSearchedCandidates(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-searched-candidates": profile}}, nil
}

// dex:field attribute-key:ashby-searched-candidates value-type:json editable:false description:"Searched candidates"
// GetDexDisplay shows the searched candidates in Dex Web.
func (*CandidateSearchFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readSearchedCandidates(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-searched-candidates": profile}}, nil
}

func readSearchedCandidates(ctx dex.Context) (ashby.CandidateSearchResults, error) {
	profile, err := searchedCandidatesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.CandidateSearchResults{}, nil
	}
	return profile, err
}

func mapToSearchCandidatesInput(input ashby.SearchCandidatesInput) ashby.SearchCandidatesInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the search fields to the candidate search Step."
type searchStartRequest struct{ dex.StepDefaults }

func (searchStartRequest) GetStepType() string { return "StartSearchCandidates" }
func (searchStartRequest) WaitFor(dex.Context, ashby.SearchCandidatesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (searchStartRequest) Execute(_ dex.Context, input ashby.SearchCandidatesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.SearchCandidatesInput](searchOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the searched candidates and complete the Flow."
type completeSearchResult struct{ dex.StepDefaultsNoWaitFor[ashby.SearchCandidatesResult] }

func (completeSearchResult) GetStepType() string { return "CompleteSearchCandidates" }
func (completeSearchResult) Execute(ctx dex.Context, result ashby.SearchCandidatesResult) (*dex.StepDecision, error) {
	if err := searchedCandidatesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateSearchFlow)(nil)
