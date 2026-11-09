// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package publishpost runs Facebook Pages publishTextPost as one Dex Flow.
package publishpost

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FacebookPagesPublishTextPost"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "facebook-pages"
	operationStepType = "CallPublishTextPost"
)

var publishedPostAttribute = dex.DefineAttribute[facebookpages.PublishedPost]("facebook-pages-publish-post-published-post")

// Flow runs PublishTextPost once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection facebookpages.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection facebookpages.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{publishedPostAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(facebookpages.NewPublishTextPostStep(facebookpages.PublishTextPostStepConfig[facebookpages.PublishTextPostInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Pages", Explanation: "Call the provider's publishTextPost operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Published:           sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:facebook-pages-publish-post-published-post value-type:json editable:false description:"Published post ID"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPublishedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-publish-post-published-post": outcome}}, nil
}

// dex:field attribute-key:facebook-pages-publish-post-published-post value-type:json editable:false description:"Published post ID"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPublishedPost(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-publish-post-published-post": outcome}}, nil
}

func readPublishedPost(ctx dex.Context) (facebookpages.PublishedPost, error) {
	outcome, err := publishedPostAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookpages.PublishedPost{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input facebookpages.PublishTextPostInput) facebookpages.PublishTextPostInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartPublishTextPost" }
func (startRequest) WaitFor(dex.Context, facebookpages.PublishTextPostInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input facebookpages.PublishTextPostInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookpages.PublishTextPostInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[facebookpages.PublishTextPostResult]
}

func (completeResult) GetStepType() string { return "CompletePublishTextPost" }
func (completeResult) Execute(ctx dex.Context, result facebookpages.PublishTextPostResult) (*dex.StepDecision, error) {
	if err := publishedPostAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
