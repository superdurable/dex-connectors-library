// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package updateissue demonstrates an absolute Sentry issue update.
package updateissue

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryUpdateIssue"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Issue]("sentry-updated-issue")

// Request identifies the issue and desired absolute fields.
type Request sentry.UpdateIssueInput

// Flow updates one issue and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the updated issue result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the updated issue.
// dex:field attribute-key:sentry-updated-issue value-type:json editable:false description:"Updated issue"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readUpdatedIssue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-updated-issue": value}}, nil
}

// GetDexDisplay returns the updated issue.
// dex:field attribute-key:sentry-updated-issue value-type:json editable:false description:"Updated issue"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readUpdatedIssue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-updated-issue": value}}, nil
}
func readUpdatedIssue(ctx dex.Context) (sentry.Issue, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Issue{}, nil
	}
	return value, err
}

// GetSteps registers the issue update path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareIssue{}),
		dex.DefineStep(sentry.NewUpdateIssueStep(sentry.UpdateIssueStepConfig[Request]{
			StepType: "UpdateIssue", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-issue", GroupLabel: "Sentry issue", Explanation: "Set an issue status for the connected Sentry organization."},
			MapToOperationInput: func(request Request) sentry.UpdateIssueInput { return sentry.UpdateIssueInput(request) },
			Updated:             sdkgo.GoTo(completeIssue{}),
		})),
		dex.DefineStep(completeIssue{}),
	}
}

// dex:group group-id:sentry-issue-prepare group-label:"Prepare issue"
// dex:explanation text:"Pass the typed issue request into the provider update."
type prepareIssue struct{ dex.StepDefaults }

func (prepareIssue) GetStepType() string { return "PrepareIssue" }
func (prepareIssue) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareIssue) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("UpdateIssue"), request), nil
}

// dex:group group-id:sentry-issue-complete group-label:"Complete issue update"
// dex:explanation text:"Complete with Sentry's updated issue metadata."
type completeIssue struct {
	dex.StepDefaultsNoWaitFor[sentry.UpdateIssueResult]
}

func (completeIssue) GetStepType() string { return "CompleteIssue" }
func (completeIssue) Execute(ctx dex.Context, result sentry.UpdateIssueResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
