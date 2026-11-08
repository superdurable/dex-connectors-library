// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package pageposts runs Facebook Pages listPosts as one Dex Flow.
package pageposts

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/facebook-pages/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FacebookPagesPagePosts"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "facebook-pages"
	operationStepType = "CallListPosts"
)

var pagePostsPageAttribute = dex.DefineAttribute[facebookpages.PagePostsPage]("facebook-pages-page-posts-page")

// Flow runs ListPosts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection facebookpages.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection facebookpages.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{pagePostsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(facebookpages.NewListPostsStep(facebookpages.ListPostsStepConfig[facebookpages.ListPostsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Facebook Pages", Explanation: "Call the provider's listPosts operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:facebook-pages-page-posts-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPagePostsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-page-posts-page": outcome}}, nil
}

// dex:field attribute-key:facebook-pages-page-posts-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readPagePostsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"facebook-pages-page-posts-page": outcome}}, nil
}

func readPagePostsPage(ctx dex.Context) (facebookpages.PagePostsPage, error) {
	outcome, err := pagePostsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return facebookpages.PagePostsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input facebookpages.ListPostsInput) facebookpages.ListPostsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListPosts" }
func (startRequest) WaitFor(dex.Context, facebookpages.ListPostsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input facebookpages.ListPostsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[facebookpages.ListPostsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[facebookpages.ListPostsResult]
}

func (completeResult) GetStepType() string { return "CompleteListPosts" }
func (completeResult) Execute(ctx dex.Context, result facebookpages.ListPostsResult) (*dex.StepDecision, error) {
	if err := pagePostsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
