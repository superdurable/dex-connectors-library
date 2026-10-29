// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuereview demonstrates one Sentry operation in a Dex Flow.
package issuereview

import (
	"github.com/superdurable/dex-connectors-library/connectors/sentry"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "SentryIssueReview"
	// ConnectionName is the static Dex Web connection for this example.
	ConnectionName    = "sentry"
	operationStepType = "ListIssues"
)

// Request is the typed start input for this example.
type Request sentry.ListIssuesInput

// Flow runs one Sentry request and completes with its typed result.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a configured Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }
func (*Flow) GetFlowType() string                { return FlowType }
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareRequest{}),
		dex.DefineStep(sentry.NewListIssuesStep(sentry.ListIssuesStepConfig[Request]{
			StepType: operationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry", GroupLabel: "Sentry", Explanation: "Call the Sentry ListIssues endpoint."},
			MapToOperationInput: func(request Request) sentry.ListIssuesInput { return sentry.ListIssuesInput(request) },
			Listed:              sdkgo.GoTo(completeRequest{}),
		})),
		dex.DefineStep(completeRequest{}),
	}
}
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema { return dex.PersistenceSchema{} }

// dex:group group-id:sentry-example group-label:"Sentry example"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:sentry-example group-label:"Sentry example"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[sentry.ListIssuesResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(_ dex.Context, result sentry.ListIssuesResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
