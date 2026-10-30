// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package outboundlookup demonstrates one Postmark operation in a Dex Flow.
package outboundlookup

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/postmark/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "PostmarkOutboundLookup"
	// ConnectionName is the static Dex Web connection for this example.
	ConnectionName    = "postmark"
	operationStepType = "ListOutboundMessages"
)

var outcomeAttribute = dex.DefineAttribute[postmark.MessagePage]("postmark-outbound-lookup-outcome")

// Request is the typed start input for this example.
type Request postmark.ListOutboundMessagesInput

// Flow runs one Postmark request and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection postmark.Connection
}

// NewFlow binds a configured Postmark connection.
func NewFlow(connection postmark.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string                  { return FlowType }
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(postmark.NewListOutboundMessagesStep(postmark.ListOutboundMessagesStepConfig[Request]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "postmark", GroupLabel: "Postmark", Explanation: "Call the Postmark ListOutboundMessages endpoint."},
			MapToOperationInput: func(request Request) postmark.ListOutboundMessagesInput {
				return postmark.ListOutboundMessagesInput(request)
			},
			Listed: sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}

// GetRPCs registers Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:postmark-outbound-lookup-outcome value-type:json editable:false description:"Outbound message page"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-outbound-lookup-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:postmark-outbound-lookup-outcome value-type:json editable:false description:"Outbound message page"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"postmark-outbound-lookup-outcome": outcome}}, nil
}

func readOutcome(ctx dex.Context) (postmark.MessagePage, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return postmark.MessagePage{}, nil
	}
	return outcome, err
}

// dex:group group-id:postmark-prepare group-label:"Prepare request"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:postmark-complete group-label:"Complete request"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[postmark.ListOutboundMessagesResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(ctx dex.Context, result postmark.ListOutboundMessagesResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
