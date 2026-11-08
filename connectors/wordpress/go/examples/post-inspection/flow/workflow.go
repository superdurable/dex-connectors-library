// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postinspection runs a WordPress published-post query as a Dex Flow.
package postinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/wordpress/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "WordPressPostInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "wordpress"
	operationStepType = "CallGetPost"
)

var inspectedPostAttribute = dex.DefineAttribute[wordpress.Post]("wordpress-inspected-post")

// Flow reads one published post and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection wordpress.Connection
}

// NewFlow binds the WordPress connection at registration time.
func NewFlow(connection wordpress.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inspectedPostAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(wordpress.NewGetPostStep(wordpress.GetPostStepConfig[wordpress.GetPostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "WordPress", Explanation: "Read the published post by ID."},
			Connection:          flow.connection,
			MapToOperationInput: mapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:wordpress-inspected-post value-type:json editable:false description:"Published post"
// GetDexSummary shows the inspected post in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readInspectedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-inspected-post": post}}, nil
}

// dex:field attribute-key:wordpress-inspected-post value-type:json editable:false description:"Published post"
// GetDexDisplay shows the inspected post in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	post, err := readInspectedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"wordpress-inspected-post": post}}, nil
}

func readInspectedPost(ctx dex.Context) (wordpress.Post, error) {
	post, err := inspectedPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return wordpress.Post{}, nil
	}
	return post, err
}

func mapToOperationInput(input wordpress.GetPostInput) wordpress.GetPostInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the post ID to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPost" }
func (startRequest) WaitFor(dex.Context, wordpress.GetPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input wordpress.GetPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[wordpress.GetPostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the published post."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[wordpress.GetPostResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPost" }
func (completeResult) Execute(ctx dex.Context, result wordpress.GetPostResult) (*dex.StepDecision, error) {
	if err := inspectedPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
