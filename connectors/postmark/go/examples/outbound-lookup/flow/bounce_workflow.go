// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// BounceFlowType identifies the runnable bounce-list Flow.
const BounceFlowType = "PostmarkBounceList"

const bounceStepType = "ListBounces"

var bouncePageAttribute = dex.DefineAttribute[postmark.BouncePage]("postmark-bounce-page")

// BounceRequest is a typed bounce page request.
type BounceRequest postmark.ListBouncesInput

// BounceFlow lists delivery failures and stores the result.
type BounceFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewBounceFlow binds the configured Postmark connection.
func NewBounceFlow(connection postmark.Connection) *BounceFlow {
	return &BounceFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*BounceFlow) GetFlowType() string { return BounceFlowType }

// GetSteps defines preparation, provider lookup, and completion Steps.
func (flow *BounceFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareBounceList{}),
		dex.DefineStep(postmark.NewListBouncesStep(postmark.ListBouncesStepConfig[BounceRequest]{
			StepType: bounceStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "postmark-bounces", GroupLabel: "List bounces", Explanation: "List Postmark delivery failures."},
			MapToOperationInput: func(request BounceRequest) postmark.ListBouncesInput { return postmark.ListBouncesInput(request) },
			Listed:              sdkgo.GoTo(completeBounceList{}),
		})),
		dex.DefineStep(completeBounceList{}),
	}
}

// GetRPCs exposes the bounce page in Dex Web.
func (flow *BounceFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored bounce page.
func (*BounceFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{bouncePageAttribute}}
}

// GetDexSummary returns the bounce page.
// dex:field attribute-key:postmark-bounce-page value-type:json editable:false description:"Postmark bounce page"
func (*BounceFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBouncePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-bounce-page": page}}, nil
}

// GetDexDisplay returns the bounce page.
// dex:field attribute-key:postmark-bounce-page value-type:json editable:false description:"Postmark bounce page"
func (*BounceFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readBouncePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-bounce-page": page}}, nil
}

func readBouncePage(ctx dex.Context) (postmark.BouncePage, error) {
	page, err := bouncePageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.BouncePage{}, nil
	}
	return page, err
}

// dex:group group-id:postmark-bounce-prepare group-label:"Prepare bounce list"
// dex:explanation text:"Pass the requested page to the provider query."
type prepareBounceList struct{ dex.StepDefaults }

func (prepareBounceList) GetStepType() string { return "PrepareBounceList" }
func (prepareBounceList) WaitFor(dex.Context, BounceRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareBounceList) Execute(_ dex.Context, request BounceRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[BounceRequest](bounceStepType), request), nil
}

// dex:group group-id:postmark-bounce-complete group-label:"Complete bounce list"
// dex:explanation text:"Persist the page and complete the Flow."
type completeBounceList struct {
	dex.StepDefaultsNoWaitFor[postmark.ListBouncesResult]
}

func (completeBounceList) GetStepType() string { return "CompleteBounceList" }
func (completeBounceList) Execute(ctx dex.Context, result postmark.ListBouncesResult) (*dex.StepDecision, error) {
	if err := bouncePageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*BounceFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*BounceFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*BounceFlow)(nil).GetDexDisplay
