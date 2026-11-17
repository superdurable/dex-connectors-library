// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package assignissue demonstrates an absolute Sentry issue assignment.
package assignissue

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/sentry/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "SentryAssignIssue"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "sentry"
)

var outcomeAttribute = dex.DefineAttribute[sentry.Issue]("sentry-assigned-issue")

// Request identifies an issue and its desired assignee.
type Request sentry.AssignIssueInput

// Flow assigns one issue and completes with its new metadata.
type Flow struct {
	dex.FlowDefaults
	connection sentry.Connection
}

// NewFlow binds a Sentry connection.
func NewFlow(connection sentry.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetPersistenceSchema registers the assigned issue result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the assigned issue.
// dex:field attribute-key:sentry-assigned-issue value-type:json editable:false description:"Assigned issue"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAssignedIssue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-assigned-issue": value}}, nil
}

// GetDexDisplay returns the assigned issue.
// dex:field attribute-key:sentry-assigned-issue value-type:json editable:false description:"Assigned issue"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readAssignedIssue(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"sentry-assigned-issue": value}}, nil
}
func readAssignedIssue(ctx dex.Context) (sentry.Issue, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return sentry.Issue{}, nil
	}
	return value, err
}

// GetSteps registers the issue assignment path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareIssue{}),
		dex.DefineStep(sentry.NewAssignIssueStep(sentry.AssignIssueStepConfig[Request]{
			StepType: "AssignIssue", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sentry-issue", GroupLabel: "Sentry issue", Explanation: "Assign the issue to the selected Sentry user or team."},
			MapToOperationInput: func(request Request) sentry.AssignIssueInput { return sentry.AssignIssueInput(request) },
			Assigned:            sdkgo.GoTo(completeIssue{}),
		})),
		dex.DefineStep(completeIssue{}),
	}
}

// dex:group group-id:sentry-issue-prepare group-label:"Prepare issue"
// dex:explanation text:"Pass the typed issue request into the provider assignment."
type prepareIssue struct{ dex.StepDefaults }

func (prepareIssue) GetStepType() string { return "PrepareIssue" }
func (prepareIssue) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareIssue) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("AssignIssue"), request), nil
}

// dex:group group-id:sentry-issue-complete group-label:"Complete issue assignment"
// dex:explanation text:"Complete with Sentry's updated issue metadata."
type completeIssue struct {
	dex.StepDefaultsNoWaitFor[sentry.AssignIssueResult]
}

func (completeIssue) GetStepType() string { return "CompleteIssue" }
func (completeIssue) Execute(ctx dex.Context, result sentry.AssignIssueResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
