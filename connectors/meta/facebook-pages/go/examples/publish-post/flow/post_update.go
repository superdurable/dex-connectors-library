// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package publishpost

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const updatePostStepType = "CallUpdatePost"

var updatedPostAttribute = dex.DefineAttribute[facebookpages.ChangedPost]("facebook-pages-updated-post")

// PostUpdateFlow replaces a Page post's message once.
type PostUpdateFlow struct {
	dex.FlowDefaults
	connection facebookpages.Connection
}

// NewPostUpdateFlow binds the static Facebook Pages connection.
func NewPostUpdateFlow(connection facebookpages.Connection) *PostUpdateFlow {
	return &PostUpdateFlow{connection: connection}
}

// GetFlowType identifies the example update Flow.
func (*PostUpdateFlow) GetFlowType() string { return "FacebookPagesUpdatePost" }

// GetPersistenceSchema registers the durable changed-post result.
func (*PostUpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedPostAttribute}}
}

// GetSteps wires the operation-specific update Mutation.
func (flow *PostUpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startPostUpdate{}),
		dex.DefineStep(facebookpages.NewUpdatePostStep(facebookpages.UpdatePostStepConfig[facebookpages.UpdatePostInput]{
			StepType: updatePostStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Pages", Explanation: "Replace a Page post message."},
			Connection:  flow.connection, MapToOperationInput: updatePostInput,
			Updated: sdkgo.GoTo(completePostUpdate{}),
		})),
		dex.DefineStep(completePostUpdate{}),
	}
}

// GetRPCs exposes the last changed post in Dex Web.
func (flow *PostUpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:facebook-pages-updated-post value-type:json editable:false description:"Updated post"
// GetDexSummary returns the last updated post ID.
func (*PostUpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readUpdatedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-updated-post": post}}, nil
}

// dex:field attribute-key:facebook-pages-updated-post value-type:json editable:false description:"Updated post"
// GetDexDisplay returns the last updated post ID.
func (*PostUpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readUpdatedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-updated-post": post}}, nil
}

func readUpdatedPost(ctx dex.Context) (facebookpages.ChangedPost, error) {
	post, err := updatedPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookpages.ChangedPost{}, nil
	}
	return post, err
}

func updatePostInput(input facebookpages.UpdatePostInput) facebookpages.UpdatePostInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the post ID and replacement message to the provider Step."
type startPostUpdate struct{ dex.StepDefaults }

func (startPostUpdate) GetStepType() string { return "StartPostUpdate" }
func (startPostUpdate) WaitFor(dex.Context, facebookpages.UpdatePostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startPostUpdate) Execute(_ dex.Context, input facebookpages.UpdatePostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookpages.UpdatePostInput](updatePostStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the updated post ID and complete the Flow."
type completePostUpdate struct {
	dex.StepDefaultsNoWaitFor[facebookpages.UpdatePostResult]
}

func (completePostUpdate) GetStepType() string { return "CompletePostUpdate" }
func (completePostUpdate) Execute(ctx dex.Context, result facebookpages.UpdatePostResult) (*dex.StepDecision, error) {
	if err := updatedPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PostUpdateFlow)(nil)
