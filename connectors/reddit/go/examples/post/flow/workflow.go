// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package reddit runs Reddit getPost as one Dex Flow.
package post

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/reddit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RedditPostLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "reddit"
	operationStepType = "CallGetPost"
)

var postAttribute = dex.DefineAttribute[reddit.Post]("reddit-post")

// Flow runs GetPost once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection reddit.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection reddit.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{postAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(reddit.NewGetPostStep(reddit.GetPostStepConfig[reddit.GetPostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Reddit", Explanation: "Read the requested post with the approved Reddit OAuth app."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:reddit-post value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"reddit-post": outcome}}, nil
}

// dex:field attribute-key:reddit-post value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"reddit-post": outcome}}, nil
}

func readPost(ctx dex.Context) (reddit.Post, error) {
	outcome, err := postAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return reddit.Post{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input reddit.GetPostInput) reddit.GetPostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetPost" }
func (startRequest) WaitFor(dex.Context, reddit.GetPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input reddit.GetPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[reddit.GetPostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[reddit.GetPostResult]
}

func (completeResult) GetStepType() string { return "CompleteGetPost" }
func (completeResult) Execute(ctx dex.Context, result reddit.GetPostResult) (*dex.StepDecision, error) {
	if err := postAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
