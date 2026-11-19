// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package opportunityinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/lever/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// PostingListFlow lists selected Lever job postings.
type PostingListFlow struct {
	dex.FlowDefaults
	connection lever.Connection
}

// NewPostingListFlow binds the provider connection at registration time.
func NewPostingListFlow(connection lever.Connection) *PostingListFlow {
	return &PostingListFlow{connection: connection}
}

func (*PostingListFlow) GetFlowType() string { return "LeverPostingList" }

var postingsAttribute = dex.DefineAttribute[lever.PostingsPage]("lever-postings")

func (*PostingListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postingsAttribute}}
}

func (flow *PostingListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPostingList{}),
		dex.DefineStep(lever.NewListPostingsStep(lever.ListPostingsStepConfig[lever.ListPostingsInput]{
			StepType: "CallListPostings", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Lever", Explanation: "List accessible job postings in the requested state."},
			Connection:  flow.connection, MapToOperationInput: mapPostingListInput,
			Listed: sdkgo.GoTo(completePostingList{}),
		})),
		dex.DefineStep(completePostingList{}),
	}
}

func (flow *PostingListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:lever-postings value-type:json editable:false description:"Job postings"
// GetDexSummary shows the selected posting page in Dex Web.
func (*PostingListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPostings(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-postings": value}}, nil
}

// dex:field attribute-key:lever-postings value-type:json editable:false description:"Job postings"
// GetDexDisplay shows the selected posting page in Dex Web.
func (*PostingListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readPostings(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"lever-postings": value}}, nil
}

func readPostings(ctx dex.Context) (lever.PostingsPage, error) {
	value, err := postingsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return lever.PostingsPage{}, nil
	}
	return value, err
}

func mapPostingListInput(input lever.ListPostingsInput) lever.ListPostingsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the optional posting state to the provider Step."
type startPostingList struct{ dex.StepDefaults }

func (startPostingList) GetStepType() string { return "StartListPostings" }
func (startPostingList) WaitFor(dex.Context, lever.ListPostingsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPostingList) Execute(_ dex.Context, input lever.ListPostingsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[lever.ListPostingsInput]("CallListPostings"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the job postings and complete the Flow."
type completePostingList struct {
	dex.StepDefaultsNoWaitFor[lever.ListPostingsResult]
}

func (completePostingList) GetStepType() string { return "CompleteListPostings" }
func (completePostingList) Execute(ctx dex.Context, result lever.ListPostingsResult) (*dex.StepDecision, error) {
	if err := postingsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PostingListFlow)(nil)
