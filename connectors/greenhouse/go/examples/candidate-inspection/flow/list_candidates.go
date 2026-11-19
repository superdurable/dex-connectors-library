// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/greenhouse/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CandidateListFlowType is the stable candidate-list Flow identity.
const CandidateListFlowType = "GreenhouseCandidateList"

var candidateListAttribute = dex.DefineAttribute[greenhouse.CandidateProfilesPage]("greenhouse-candidate-list")

// CandidateListFlow reads one page of candidate IDs and names.
type CandidateListFlow struct {
	dex.FlowDefaults
	connection greenhouse.Connection
}

// NewCandidateListFlow binds the provider connection at registration time.
func NewCandidateListFlow(connection greenhouse.Connection) *CandidateListFlow {
	return &CandidateListFlow{connection: connection}
}

func (*CandidateListFlow) GetFlowType() string { return CandidateListFlowType }

func (*CandidateListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{candidateListAttribute}}
}

func (flow *CandidateListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCandidateList{}),
		dex.DefineStep(greenhouse.NewListCandidatesStep(greenhouse.ListCandidatesStepConfig[greenhouse.ListCandidatesInput]{
			StepType: "CallListCandidates", ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Greenhouse", Explanation: "Read one page of candidate names."},
			Connection:          flow.connection,
			MapToOperationInput: func(input greenhouse.ListCandidatesInput) greenhouse.ListCandidatesInput { return input },
			Listed:              sdkgo.GoTo(completeCandidateList{}),
		})),
		dex.DefineStep(completeCandidateList{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *CandidateListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:greenhouse-candidate-list value-type:json editable:false description:"Candidate page"
// GetDexSummary shows the last candidate page in Dex Web.
func (*CandidateListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCandidateList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"greenhouse-candidate-list": page}}, nil
}

// dex:field attribute-key:greenhouse-candidate-list value-type:json editable:false description:"Candidate page"
// GetDexDisplay shows the last candidate page in Dex Web.
func (*CandidateListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCandidateList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"greenhouse-candidate-list": page}}, nil
}

func readCandidateList(ctx dex.Context) (greenhouse.CandidateProfilesPage, error) {
	page, err := candidateListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return greenhouse.CandidateProfilesPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page settings to the provider Step."
type startCandidateList struct{ dex.StepDefaults }

func (startCandidateList) GetStepType() string { return "StartCandidateList" }
func (startCandidateList) WaitFor(dex.Context, greenhouse.ListCandidatesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCandidateList) Execute(_ dex.Context, input greenhouse.ListCandidatesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[greenhouse.ListCandidatesInput]("CallListCandidates"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected candidate page."
type completeCandidateList struct {
	dex.StepDefaultsNoWaitFor[greenhouse.ListCandidatesResult]
}

func (completeCandidateList) GetStepType() string { return "CompleteCandidateList" }
func (completeCandidateList) Execute(ctx dex.Context, result greenhouse.ListCandidatesResult) (*dex.StepDecision, error) {
	if err := candidateListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CandidateListFlow)(nil)
