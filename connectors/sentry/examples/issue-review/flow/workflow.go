// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuereview demonstrates one Sentry operation in a Dex Flow.
package issuereview

import (
	"errors"
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

var outcomeAttribute = dex.DefineAttribute[sentry.IssuePage]("sentry-issue-review-outcome")

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
// dex:field attribute-key:sentry-issue-review-outcome value-type:json editable:false description:"Issue review page"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-issue-review-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:sentry-issue-review-outcome value-type:json editable:false description:"Issue review page"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-issue-review-outcome": outcome}}, nil
}

func readOutcome(ctx dex.Context) (sentry.IssuePage, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.IssuePage{}, nil
	}
	return outcome, err
}

// dex:group group-id:sentry-prepare group-label:"Prepare request"
// dex:explanation text:"Persist the typed start input before the provider operation."
type prepareRequest struct{ dex.StepDefaults }

func (prepareRequest) GetStepType() string { return "PrepareRequest" }
func (prepareRequest) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareRequest) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request](operationStepType), request), nil
}

// dex:group group-id:sentry-complete group-label:"Complete request"
// dex:explanation text:"Complete the Flow with the provider result."
type completeRequest struct {
	dex.StepDefaultsNoWaitFor[sentry.ListIssuesResult]
}

func (completeRequest) GetStepType() string { return "CompleteRequest" }
func (completeRequest) Execute(ctx dex.Context, result sentry.ListIssuesResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
