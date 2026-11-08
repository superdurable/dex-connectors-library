// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postcreation creates one WordPress post as a Dex Flow.
package postcreation

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WordPressPostCreation"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "wordpress"
	operationStepType = "CallCreatePost"
)

var createdPostAttribute = dex.DefineAttribute[wordpress.CreatedPost]("wordpress-created-post")

// Flow creates one draft and completes with its provider identity.
type Flow struct {
	dex.FlowDefaults
	connection wordpress.Connection
}

// NewFlow binds the WordPress connection at registration time.
func NewFlow(connection wordpress.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createdPostAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wordpress.NewCreatePostStep(wordpress.CreatePostStepConfig[wordpress.CreatePostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WordPress", Explanation: "Create one draft post for editorial review."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Created:             sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wordpress-created-post value-type:json editable:false description:"Created post"
// GetDexSummary shows the created post identity in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readCreatedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-created-post": post}}, nil
}

// dex:field attribute-key:wordpress-created-post value-type:json editable:false description:"Created post"
// GetDexDisplay shows the created post identity in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readCreatedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-created-post": post}}, nil
}

func readCreatedPost(ctx dex.Context) (wordpress.CreatedPost, error) {
	post, err := createdPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wordpress.CreatedPost{}, nil
	}
	return post, err
}

func mapToOperationInput(input wordpress.CreatePostInput) wordpress.CreatePostInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the draft input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartCreatePost" }
func (startRequest) WaitFor(dex.Context, wordpress.CreatePostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wordpress.CreatePostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wordpress.CreatePostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the created post."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wordpress.CreatePostResult]
}

func (completeResult) GetStepType() string { return "CompleteCreatePost" }
func (completeResult) Execute(ctx dex.Context, result wordpress.CreatePostResult) (*dex.StepDecision, error) {
	if err := createdPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
