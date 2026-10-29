// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package outboundlookup demonstrates one Postmark operation in a Dex Flow.
package outboundlookup

import (
	"github.com/superdurable/dex-connectors-library/connectors/postmark"
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
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:postmark-example group-label:"Postmark example"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:postmark-example group-label:"Postmark example"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[postmark.ListOutboundMessagesResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(_ dex.Context, result postmark.ListOutboundMessagesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
