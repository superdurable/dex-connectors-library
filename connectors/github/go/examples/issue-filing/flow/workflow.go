// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuefiling demonstrates the GitHub listIssues and createIssue operations.
package issuefiling

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/github/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow type shared by Dex Web Start Flow, the Worker registry, and open runs.
	FlowType = "GitHubIssueFiling"
	// ConnectionName names the connection; it must use the github-oauth-repository method.
	ConnectionName         = "github-issue-filing"
	listOpenIssuesStepType = "ListOpenIssues"
	createIssueStepType    = "CreateIssue"
	// Application Step types match the Flow Definition Graph so Dex Web starts and displays the same Steps.
	startIssueFilingStepType    = "startIssueFiling"
	recordOpenIssuesStepType    = "recordOpenIssues"
	completeIssueFilingStepType = "completeIssueFiling"
	// OpenIssuePageSize is the number of open issues read per page.
	OpenIssuePageSize = 100
	// MaxOpenIssuePages bounds the open issue pages checked for a matching title before creating.
	MaxOpenIssuePages = 3
)

var reportAttribute = dex.DefineAttribute[Report]("github-issue-filing-report")

// Status is the stage of one issue filing.
type Status string

const (
	StatusCheckingOpenIssues Status = "checkingOpenIssues"
	StatusCreatingIssue      Status = "creatingIssue"
	StatusAlreadyTracked     Status = "alreadyTracked"
	StatusCreated            Status = "created"
)

// Input names the repository and the issue to file unless an open issue with its title already exists.
type Input struct {
	Owner      string   `json:"owner"`
	Repository string   `json:"repository"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Labels     []string `json:"labels"`
}

// OpenIssuePageRequest is the durable input of one open issue page read.
type OpenIssuePageRequest struct {
	Owner      string   `json:"owner"`
	Repository string   `json:"repository"`
	Labels     []string `json:"labels"`
	Page       int      `json:"page"`
}

// Report is the durable record of one issue filing that the Flow returns.
type Report struct {
	Input              Input  `json:"input"`
	Status             Status `json:"status"`
	OpenIssuePagesRead int    `json:"openIssuePagesRead"`
	// HasUncheckedOpenIssues reports that open issues beyond MaxOpenIssuePages were not checked.
	HasUncheckedOpenIssues bool                 `json:"hasUncheckedOpenIssues"`
	ExistingIssue          *github.Issue        `json:"existingIssue,omitempty"`
	CreatedIssue           *github.CreatedIssue `json:"createdIssue,omitempty"`
}

type Flow struct {
	dex.FlowDefaults
	connection github.Connection
}

func NewFlow(connection github.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType overrides the SDK's package-qualified default so that Dex Web starts the type this Worker
// registers.
func (*Flow) GetFlowType() string {
	return FlowType
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startIssueFiling{}),
		dex.DefineStep(github.NewListIssuesStep(github.ListIssuesStepConfig[OpenIssuePageRequest]{
			StepType: listOpenIssuesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "duplicate-check", GroupLabel: "Duplicate check", Explanation: "List one page of open issues that carry the requested labels."},
			Connection:  flow.connection,
			MapToOperationInput: func(request OpenIssuePageRequest) github.ListIssuesInput {
				return github.ListIssuesInput{
					Owner: request.Owner, Repository: request.Repository, State: "open", Labels: request.Labels,
					PageSize: OpenIssuePageSize, Page: request.Page,
				}
			},
			Listed: sdkgo.GoTo(recordOpenIssues{}),
		})),
		dex.DefineStep(recordOpenIssues{}),
		dex.DefineStep(github.NewCreateIssueStep(github.CreateIssueStepConfig[Input]{
			StepType: createIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "issue-creation", GroupLabel: "Issue creation", Explanation: "Create the issue once, reading back a retried send instead of filing a duplicate."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) github.CreateIssueInput {
				return github.CreateIssueInput{
					Owner: input.Owner, Repository: input.Repository, Title: input.Title, Body: input.Body, Labels: input.Labels,
				}
			},
			Created: sdkgo.GoTo(completeIssueFiling{}),
		})),
		dex.DefineStep(completeIssueFiling{}),
	}
}

func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{reportAttribute}}
}

// dex:field attribute-key:github-issue-filing-report value-type:json editable:false description:"GitHub issue filing status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-issue-filing-report": report}}, nil
}

// dex:field attribute-key:github-issue-filing-report value-type:json editable:false description:"GitHub issue filing report"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-issue-filing-report": report}}, nil
}

// optionalReport returns an empty report before the start Step records one.
func optionalReport(ctx dex.Context) (Report, error) {
	report, err := reportAttribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		return Report{}, nil
	}
	return report, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Record the requested issue before checking the open issues."
type startIssueFiling struct {
	dex.StepDefaults
}

func (startIssueFiling) GetStepType() string { return startIssueFilingStepType }

// WaitFor skips at once; Dex Web Start Flow calls it, and execute-only start Steps reject that.
func (startIssueFiling) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startIssueFiling) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := reportAttribute.Set(ctx, Report{Input: input, Status: StatusCheckingOpenIssues}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[OpenIssuePageRequest](listOpenIssuesStepType), openIssuePageRequest(input, 1)), nil
}

// dex:group group-id:duplicate-check group-label:"Duplicate check"
// dex:explanation text:"Complete when an open issue has the title; otherwise read the next page or create the issue."
type recordOpenIssues struct {
	dex.StepDefaultsNoWaitFor[github.ListIssuesResult]
}

func (recordOpenIssues) GetStepType() string { return recordOpenIssuesStepType }

func (recordOpenIssues) Execute(ctx dex.Context, result github.ListIssuesResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.OpenIssuePagesRead++
	if existing, isTracked := findIssueWithTitle(result.Value.Issues, report.Input.Title); isTracked {
		report.ExistingIssue, report.Status = &existing, StatusAlreadyTracked
		if err := reportAttribute.Set(ctx, report); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(report), nil
	}
	if result.Value.NextPage != 0 && report.OpenIssuePagesRead < MaxOpenIssuePages {
		if err := reportAttribute.Set(ctx, report); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[OpenIssuePageRequest](listOpenIssuesStepType), openIssuePageRequest(report.Input, result.Value.NextPage)), nil
	}
	report.HasUncheckedOpenIssues = result.Value.NextPage != 0
	report.Status = StatusCreatingIssue
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](createIssueStepType), report.Input), nil
}

// dex:group group-id:issue-creation group-label:"Issue creation"
// dex:explanation text:"Record the created issue and complete the filing."
type completeIssueFiling struct {
	dex.StepDefaultsNoWaitFor[github.CreateIssueResult]
}

func (completeIssueFiling) GetStepType() string { return completeIssueFilingStepType }

func (completeIssueFiling) Execute(ctx dex.Context, result github.CreateIssueResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	created := result.Value
	report.CreatedIssue, report.Status = &created, StatusCreated
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

func openIssuePageRequest(input Input, page int) OpenIssuePageRequest {
	return OpenIssuePageRequest{Owner: input.Owner, Repository: input.Repository, Labels: input.Labels, Page: page}
}

// findIssueWithTitle returns the first issue whose title matches, ignoring case and surrounding whitespace.
func findIssueWithTitle(issues []github.Issue, title string) (github.Issue, bool) {
	for _, issue := range issues {
		if strings.EqualFold(strings.TrimSpace(issue.Title), strings.TrimSpace(title)) {
			return issue, true
		}
	}
	return github.Issue{}, false
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
