// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resourceread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/atlassian/bitbucket/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const createPullRequestCommentStepType = "CreateBitbucketPullRequestComment"

var createdPullRequestCommentAttribute = dex.DefineAttribute[bitbucket.PullRequestComment]("bitbucket-created-pull-request-comment")

// PullRequestCommentCreateFlow creates a Bitbucket pull request comment.
type PullRequestCommentCreateFlow struct {
	dex.FlowDefaults
	connection bitbucket.Connection
}

// NewPullRequestCommentCreateFlow binds the static provider connection.
func NewPullRequestCommentCreateFlow(connection bitbucket.Connection) *PullRequestCommentCreateFlow {
	return &PullRequestCommentCreateFlow{connection: connection}
}

// GetFlowType returns the stable comment create Flow identity.
func (*PullRequestCommentCreateFlow) GetFlowType() string { return "BitbucketPullRequestCommentCreate" }

// GetPersistenceSchema registers the retained comment.
func (*PullRequestCommentCreateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdPullRequestCommentAttribute}}
}

// GetSteps registers the start, provider Mutation, and completion Steps.
func (flow *PullRequestCommentCreateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPullRequestCommentCreate{}),
		dex.DefineStep(bitbucket.NewCreatePullRequestCommentStep(bitbucket.CreatePullRequestCommentStepConfig[bitbucket.CreatePullRequestCommentInput]{
			StepType: createPullRequestCommentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "bitbucket", GroupLabel: "Bitbucket", Explanation: "Create a Markdown comment on a pull request."},
			MapToOperationInput: mapToPullRequestCommentCreateInput,
			Created:             sdkgo.GoTo(completePullRequestCommentCreate{}),
		})),
		dex.DefineStep(completePullRequestCommentCreate{}),
	}
}

// GetRPCs exposes read-only Dex Web views of the created comment.
func (flow *PullRequestCommentCreateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bitbucket-created-pull-request-comment value-type:json editable:false description:"Created pull request comment"
// GetDexSummary shows the created comment in Dex Web.
func (*PullRequestCommentCreateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	comment, err := readCreatedPullRequestComment(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-pull-request-comment": comment}}, nil
}

// dex:field attribute-key:bitbucket-created-pull-request-comment value-type:json editable:false description:"Created pull request comment"
// GetDexDisplay shows the created comment in Dex Web.
func (*PullRequestCommentCreateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	comment, err := readCreatedPullRequestComment(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bitbucket-created-pull-request-comment": comment}}, nil
}

func readCreatedPullRequestComment(ctx dex.Context) (bitbucket.PullRequestComment, error) {
	comment, err := createdPullRequestCommentAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bitbucket.PullRequestComment{}, nil
	}
	return comment, err
}

func mapToPullRequestCommentCreateInput(input bitbucket.CreatePullRequestCommentInput) bitbucket.CreatePullRequestCommentInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the pull request identity and Markdown text to the provider Step."
type startPullRequestCommentCreate struct{ dex.StepDefaults }

func (startPullRequestCommentCreate) GetStepType() string { return "StartPullRequestCommentCreate" }
func (startPullRequestCommentCreate) WaitFor(dex.Context, bitbucket.CreatePullRequestCommentInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPullRequestCommentCreate) Execute(_ dex.Context, input bitbucket.CreatePullRequestCommentInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bitbucket.CreatePullRequestCommentInput](createPullRequestCommentStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Persist the created comment and complete the Flow."
type completePullRequestCommentCreate struct {
	dex.StepDefaultsNoWaitFor[bitbucket.CreatePullRequestCommentResult]
}

func (completePullRequestCommentCreate) GetStepType() string {
	return "CompletePullRequestCommentCreate"
}
func (completePullRequestCommentCreate) Execute(ctx dex.Context, result bitbucket.CreatePullRequestCommentResult) (*dex.StepDecision, error) {
	if err := createdPullRequestCommentAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PullRequestCommentCreateFlow)(nil)
