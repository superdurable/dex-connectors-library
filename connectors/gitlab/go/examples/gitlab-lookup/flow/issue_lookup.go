// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlablookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/gitlab/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// IssueFlowType is the stable identity of the issue lookup example.
const IssueFlowType = "GitLabIssueLookup"

const issueStepType = "GetIssueGitLab"

var issueRequestAttribute = dex.DefineAttribute[gitlab.GetIssueInput]("gitlab-issue-lookup-request")
var issueOutputAttribute = dex.DefineAttribute[gitlab.Issue]("gitlab-issue-lookup-output")

// IssueFlow reads a project issue and stores its typed result.
type IssueFlow struct {
	dex.FlowDefaults
	connection gitlab.Connection
}

// NewIssueFlow binds the GitLab connection at registration time.
func NewIssueFlow(connection gitlab.Connection) *IssueFlow { return &IssueFlow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*IssueFlow) GetFlowType() string { return IssueFlowType }

// GetPersistenceSchema registers both issue lookup Attributes.
func (*IssueFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{issueRequestAttribute, issueOutputAttribute}}
}

// GetRPCs registers read-only issue inspection RPCs.
func (flow *IssueFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the stored request and result when available.
// dex:field attribute-key:gitlab-issue-lookup-request value-type:json editable:false description:"GitLab issue lookup request"
// dex:field attribute-key:gitlab-issue-lookup-output value-type:json editable:false description:"GitLab issue lookup result"
func (*IssueFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := issueInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitlab-issue-lookup-request": request, "gitlab-issue-lookup-output": output}}, nil
}

// GetDexDisplay returns the stored request and result when available.
// dex:field attribute-key:gitlab-issue-lookup-request value-type:json editable:false description:"GitLab issue lookup request"
// dex:field attribute-key:gitlab-issue-lookup-output value-type:json editable:false description:"GitLab issue lookup result"
func (*IssueFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, output, err := issueInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"gitlab-issue-lookup-request": request, "gitlab-issue-lookup-output": output}}, nil
}

func issueInspection(ctx dex.Context) (gitlab.GetIssueInput, gitlab.Issue, error) {
	request, err := issueRequestAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		err = nil
	}
	if err != nil {
		return gitlab.GetIssueInput{}, gitlab.Issue{}, err
	}
	output, err := issueOutputAttribute.Get(ctx)
	if errors.As(err, &missing) {
		err = nil
	}
	return request, output, err
}

// GetSteps registers the issue start, provider, and completion Steps.
func (flow *IssueFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIssueLookup{}),
		dex.DefineStep(gitlab.NewGetIssueStep(gitlab.GetIssueStepConfig[gitlab.GetIssueInput]{
			StepType: issueStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "gitlab", GroupLabel: "GitLab", Explanation: "Read one project issue by its project-local issue number."},
			MapToOperationInput: func(input gitlab.GetIssueInput) gitlab.GetIssueInput { return input },
			Found:               sdkgo.GoTo(completeIssueLookup{}),
		})),
		dex.DefineStep(completeIssueLookup{}),
	}
}

// dex:group group-id:gitlab group-label:"GitLab"
// dex:explanation text:"Start an issue lookup with a project and issue number."
type startIssueLookup struct{ dex.StepDefaults }

func (startIssueLookup) GetStepType() string { return "StartGitLabIssueLookup" }
func (startIssueLookup) WaitFor(dex.Context, gitlab.GetIssueInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startIssueLookup) Execute(ctx dex.Context, input gitlab.GetIssueInput) (*dex.StepDecision, error) {
	if err := issueRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[gitlab.GetIssueInput](issueStepType), input), nil
}

// dex:group group-id:gitlab group-label:"GitLab"
// dex:explanation text:"Complete the issue lookup with the GitLab result."
type completeIssueLookup struct {
	dex.StepDefaultsNoWaitFor[gitlab.GetIssueResult]
}

func (completeIssueLookup) GetStepType() string { return "CompleteGitLabIssueLookup" }
func (completeIssueLookup) Execute(ctx dex.Context, result gitlab.GetIssueResult) (*dex.StepDecision, error) {
	if err := issueOutputAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*IssueFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*IssueFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*IssueFlow)(nil).GetDexDisplay
