// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postcreation

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var updatedPostAttribute = dex.DefineAttribute[wordpress.CreatedPost]("wordpress-updated-post")

// PostUpdateFlow demonstrates an absolute editorial update.
type PostUpdateFlow struct {
	dex.FlowDefaults
	connection wordpress.Connection
}

// NewPostUpdateFlow binds the static WordPress connection.
func NewPostUpdateFlow(connection wordpress.Connection) *PostUpdateFlow {
	return &PostUpdateFlow{connection: connection}
}

// GetPersistenceSchema registers the confirmed provider outcome.
func (*PostUpdateFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updatedPostAttribute}}
}

// GetSteps registers input validation, provider update, and completion.
func (flow *PostUpdateFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(preparePostUpdate{}),
		dex.DefineStep(wordpress.NewUpdatePostStep(wordpress.UpdatePostStepConfig[wordpress.UpdatePostInput]{
			StepType: "CallUpdatePost", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "post-update", GroupLabel: "Post update", Explanation: "Replace a WordPress post's editorial fields."},
			MapToOperationInput: func(input wordpress.UpdatePostInput) wordpress.UpdatePostInput { return input },
			Updated:             sdkgo.GoTo(completePostUpdate{}),
		})),
		dex.DefineStep(completePostUpdate{}),
	}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *PostUpdateFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wordpress-updated-post value-type:json editable:false description:"Updated post"
// GetDexSummary reads the confirmed post update.
func (*PostUpdateFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := updatedPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-updated-post": post}}, nil
}

// dex:field attribute-key:wordpress-updated-post value-type:json editable:false description:"Updated post"
// GetDexDisplay reads the confirmed post update.
func (*PostUpdateFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := updatedPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missing) {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-updated-post": post}}, nil
}

// dex:group group-id:post-update group-label:"Post update"
// dex:explanation text:"Validate the target post and replacement fields."
type preparePostUpdate struct{ dex.StepDefaults }

func (preparePostUpdate) WaitFor(dex.Context, wordpress.UpdatePostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (preparePostUpdate) Execute(_ dex.Context, input wordpress.UpdatePostInput) (*dex.StepDecision, error) {
	if input.PostID <= 0 || input.Title == "" {
		return dex.ForceFail("postId and title are required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[wordpress.UpdatePostInput]("CallUpdatePost"), input), nil
}

// dex:group group-id:post-update-result group-label:"Post update result"
// dex:explanation text:"Persist the confirmed WordPress post update."
type completePostUpdate struct {
	dex.StepDefaultsNoWaitFor[wordpress.UpdatePostResult]
}

func (completePostUpdate) Execute(ctx dex.Context, result wordpress.UpdatePostResult) (*dex.StepDecision, error) {
	if err := updatedPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*PostUpdateFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*PostUpdateFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*PostUpdateFlow)(nil).GetDexDisplay
