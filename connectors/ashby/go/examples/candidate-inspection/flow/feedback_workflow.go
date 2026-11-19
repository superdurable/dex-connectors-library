// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package candidateinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/ashby/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const requestFeedbackStepType = "CallRequestFeedback"

var requestedFeedbackAttribute = dex.DefineAttribute[ashby.FeedbackRequestReceipt]("ashby-requested-feedback")

// FeedbackRequestFlow requests interviewer feedback for an application.
type FeedbackRequestFlow struct {
	dex.FlowDefaults
	connection ashby.Connection
}

// NewFeedbackRequestFlow binds the provider connection at registration time.
func NewFeedbackRequestFlow(connection ashby.Connection) *FeedbackRequestFlow {
	return &FeedbackRequestFlow{connection: connection}
}

// GetFlowType returns the stable example identity.
func (*FeedbackRequestFlow) GetFlowType() string { return "AshbyFeedbackRequest" }

// GetPersistenceSchema registers the retained feedback request receipt.
func (*FeedbackRequestFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestedFeedbackAttribute}}
}

// GetSteps registers the start, create Mutation, and completion Steps.
func (flow *FeedbackRequestFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(requestFeedbackStart{}),
		dex.DefineStep(ashby.NewRequestApplicationFeedbackStep(ashby.RequestApplicationFeedbackStepConfig[ashby.RequestApplicationFeedbackInput]{
			StepType: requestFeedbackStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Ashby", Explanation: "Request feedback in Ashby."},
			Connection:  flow.connection, MapToOperationInput: mapToRequestApplicationFeedbackInput,
			Requested: sdkgo.GoTo(completeFeedbackRequest{}),
		})),
		dex.DefineStep(completeFeedbackRequest{}),
	}
}

// GetRPCs exposes read-only Dex Web views.
func (flow *FeedbackRequestFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:ashby-requested-feedback value-type:json editable:false description:"Requested feedback"
// GetDexSummary shows the requested feedback in Dex Web.
func (*FeedbackRequestFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := readRequestedFeedback(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-requested-feedback": receipt}}, nil
}

// dex:field attribute-key:ashby-requested-feedback value-type:json editable:false description:"Requested feedback"
// GetDexDisplay shows the requested feedback in Dex Web.
func (*FeedbackRequestFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	receipt, err := readRequestedFeedback(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"ashby-requested-feedback": receipt}}, nil
}

func readRequestedFeedback(ctx dex.Context) (ashby.FeedbackRequestReceipt, error) {
	receipt, err := requestedFeedbackAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return ashby.FeedbackRequestReceipt{}, nil
	}
	return receipt, err
}

func mapToRequestApplicationFeedbackInput(input ashby.RequestApplicationFeedbackInput) ashby.RequestApplicationFeedbackInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the application, interview, and interviewer IDs to the request Step."
type requestFeedbackStart struct{ dex.StepDefaults }

func (requestFeedbackStart) GetStepType() string { return "StartRequestFeedback" }
func (requestFeedbackStart) WaitFor(dex.Context, ashby.RequestApplicationFeedbackInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (requestFeedbackStart) Execute(_ dex.Context, input ashby.RequestApplicationFeedbackInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[ashby.RequestApplicationFeedbackInput](requestFeedbackStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Retain the requested feedback and complete the Flow."
type completeFeedbackRequest struct {
	dex.StepDefaultsNoWaitFor[ashby.RequestApplicationFeedbackResult]
}

func (completeFeedbackRequest) GetStepType() string { return "CompleteFeedbackRequest" }
func (completeFeedbackRequest) Execute(ctx dex.Context, result ashby.RequestApplicationFeedbackResult) (*dex.StepDecision, error) {
	if err := requestedFeedbackAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FeedbackRequestFlow)(nil)
