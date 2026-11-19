// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meistertask/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const commentListingStepType = "CallListComments"

var commentListingAttribute = dex.DefineAttribute[meistertask.CommentsPage]("meistertask-comment-list")

// CommentListingFlow reads one page of comments for a Dex application.
type CommentListingFlow struct {
	dex.FlowDefaults
	connection meistertask.Connection
}

// NewCommentListingFlow binds the same static MeisterTask connection used by comment inspection.
func NewCommentListingFlow(connection meistertask.Connection) *CommentListingFlow {
	return &CommentListingFlow{connection: connection}
}

// GetFlowType identifies the comment listing Flow.
func (*CommentListingFlow) GetFlowType() string { return "MeisterTaskCommentListing" }

// GetPersistenceSchema registers its durable provider result.
func (*CommentListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{commentListingAttribute}}
}

// GetSteps wires the comment listing Query through an operation-specific factory.
func (flow *CommentListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCommentListing{}),
		dex.DefineStep(meistertask.NewListCommentsStep(meistertask.ListCommentsStepConfig[meistertask.ListCommentsInput]{
			StepType: commentListingStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "MeisterTask", Explanation: "List comments visible to the connection."},
			Connection:  flow.connection, MapToOperationInput: listCommentsInput,
			Listed: sdkgo.GoTo(completeCommentListing{}),
		})),
		dex.DefineStep(completeCommentListing{}),
	}
}

// GetRPCs exposes the comment page in Dex Web.
func (flow *CommentListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meistertask-comment-list value-type:json editable:false description:"Comment page"
// GetDexSummary returns the last comment page.
func (*CommentListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	comments, err := readCommentListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-comment-list": comments}}, nil
}

// dex:field attribute-key:meistertask-comment-list value-type:json editable:false description:"Comment page"
// GetDexDisplay returns the last comment page.
func (*CommentListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	comments, err := readCommentListing(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meistertask-comment-list": comments}}, nil
}

func readCommentListing(ctx dex.Context) (meistertask.CommentsPage, error) {
	comments, err := commentListingAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return meistertask.CommentsPage{}, nil
	}
	return comments, err
}

func listCommentsInput(input meistertask.ListCommentsInput) meistertask.ListCommentsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass page filters to the MeisterTask list operation."
type startCommentListing struct{ dex.StepDefaults }

func (startCommentListing) GetStepType() string { return "StartCommentListing" }
func (startCommentListing) WaitFor(dex.Context, meistertask.ListCommentsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCommentListing) Execute(_ dex.Context, input meistertask.ListCommentsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[meistertask.ListCommentsInput](commentListingStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the returned comment page and complete the Flow."
type completeCommentListing struct {
	dex.StepDefaultsNoWaitFor[meistertask.ListCommentsResult]
}

func (completeCommentListing) GetStepType() string { return "CompleteCommentListing" }
func (completeCommentListing) Execute(ctx dex.Context, result meistertask.ListCommentsResult) (*dex.StepDecision, error) {
	if err := commentListingAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CommentListingFlow)(nil)
