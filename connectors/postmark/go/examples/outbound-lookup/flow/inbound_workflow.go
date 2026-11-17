// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outboundlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// InboundFlowType identifies the runnable inbound-list Flow.
const InboundFlowType = "PostmarkInboundList"

const inboundStepType = "ListInboundMessages"

var inboundPageAttribute = dex.DefineAttribute[postmark.InboundMessagePage]("postmark-inbound-page")

// InboundRequest is a typed inbound message page request.
type InboundRequest postmark.ListInboundMessagesInput

// InboundFlow lists inbound activity and stores the result.
type InboundFlow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewInboundFlow binds the configured Postmark connection.
func NewInboundFlow(connection postmark.Connection) *InboundFlow {
	return &InboundFlow{connection: connection}
}

// GetFlowType returns the stable example Flow identity.
func (*InboundFlow) GetFlowType() string { return InboundFlowType }

// GetSteps defines preparation, provider lookup, and completion Steps.
func (flow *InboundFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareInboundList{}),
		dex.DefineStep(postmark.NewListInboundMessagesStep(postmark.ListInboundMessagesStepConfig[InboundRequest]{
			StepType: inboundStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "postmark-inbound", GroupLabel: "List inbound messages", Explanation: "List Postmark inbound activity."},
			MapToOperationInput: func(request InboundRequest) postmark.ListInboundMessagesInput {
				return postmark.ListInboundMessagesInput(request)
			},
			Listed: sdkgo.GoTo(completeInboundList{}),
		})),
		dex.DefineStep(completeInboundList{}),
	}
}

// GetRPCs exposes the inbound message page in Dex Web.
func (flow *InboundFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the stored inbound message page.
func (*InboundFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inboundPageAttribute}}
}

// GetDexSummary returns the inbound message page.
// dex:field attribute-key:postmark-inbound-page value-type:json editable:false description:"Postmark inbound message page"
func (*InboundFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readInboundMessagePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-inbound-page": page}}, nil
}

// GetDexDisplay returns the inbound message page.
// dex:field attribute-key:postmark-inbound-page value-type:json editable:false description:"Postmark inbound message page"
func (*InboundFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readInboundMessagePage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-inbound-page": page}}, nil
}

func readInboundMessagePage(ctx dex.Context) (postmark.InboundMessagePage, error) {
	page, err := inboundPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.InboundMessagePage{}, nil
	}
	return page, err
}

// dex:group group-id:postmark-inbound-prepare group-label:"Prepare inbound message list"
// dex:explanation text:"Pass the requested page to the provider query."
type prepareInboundList struct{ dex.StepDefaults }

func (prepareInboundList) GetStepType() string { return "PrepareInboundList" }
func (prepareInboundList) WaitFor(dex.Context, InboundRequest) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareInboundList) Execute(_ dex.Context, request InboundRequest) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[InboundRequest](inboundStepType), request), nil
}

// dex:group group-id:postmark-inbound-complete group-label:"Complete inbound message list"
// dex:explanation text:"Persist the page and complete the Flow."
type completeInboundList struct {
	dex.StepDefaultsNoWaitFor[postmark.ListInboundMessagesResult]
}

func (completeInboundList) GetStepType() string { return "CompleteInboundList" }
func (completeInboundList) Execute(ctx dex.Context, result postmark.ListInboundMessagesResult) (*dex.StepDecision, error) {
	if err := inboundPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*InboundFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*InboundFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*InboundFlow)(nil).GetDexDisplay
