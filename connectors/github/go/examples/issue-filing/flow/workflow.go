// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package issuefiling demonstrates the GitHub listIssues, createIssue, createIssueComment, replaceIssueLabels, and
// setIssueState operations.
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
	closeIssueStepType     = "CloseIssue"
	commentOnIssueStepType = "CommentOnIssue"
	relabelIssueStepType   = "RelabelIssue"
	// Application Step types match the Flow Definition Graph so Dex Web starts and displays the same Steps.
	startIssueFilingStepType    = "startIssueFiling"
	recordOpenIssuesStepType    = "recordOpenIssues"
	completeIssueFilingStepType = "completeIssueFiling"
	recordClosedIssueStepType   = "recordClosedIssue"
	recordIssueCommentStepType  = "recordIssueComment"
	recordIssueLabelsStepType   = "recordIssueLabels"
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
	StatusCommentingOnIssue  Status = "commentingOnIssue"
	StatusRelabelingIssue    Status = "relabelingIssue"
	StatusClosingIssue       Status = "closingIssue"
	StatusClosed             Status = "closed"
)

// Input names the repository and the issue to file unless an open issue with its title already exists.
type Input struct {
	Owner      string   `json:"owner"`
	Repository string   `json:"repository"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Labels     []string `json:"labels"`
	// CloseWithStateReason, completed or not_planned, closes the created issue at once, filing a problem that
	// is already resolved. Empty leaves the issue open.
	CloseWithStateReason string `json:"closeWithStateReason,omitempty"`
	// Comment, when not blank, is added to the created issue as a Markdown comment.
	Comment string `json:"comment,omitempty"`
	// RelabelWith, when not empty, replaces the created issue's labels with these names.
	RelabelWith []string `json:"relabelWith,omitempty"`
}

// CloseIssueRequest is the durable input of closing the created issue.
type CloseIssueRequest struct {
	Owner       string `json:"owner"`
	Repository  string `json:"repository"`
	IssueNumber int    `json:"issueNumber"`
	StateReason string `json:"stateReason"`
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
	HasUncheckedOpenIssues bool                        `json:"hasUncheckedOpenIssues"`
	ExistingIssue          *github.Issue               `json:"existingIssue,omitempty"`
	CreatedIssue           *github.CreatedIssue        `json:"createdIssue,omitempty"`
	ClosedIssue            *github.UpdatedIssueState   `json:"closedIssue,omitempty"`
	IssueComment           *github.CreatedIssueComment `json:"issueComment,omitempty"`
	RelabeledIssue         *github.ReplacedIssueLabels `json:"relabeledIssue,omitempty"`
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
		dex.DefineStep(github.NewCreateIssueCommentStep(github.CreateIssueCommentStepConfig[github.CreateIssueCommentInput]{
			StepType: commentOnIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "issue-comment", GroupLabel: "Issue comment", Explanation: "Comment on the created issue once, reading back a retried send instead of posting a duplicate."},
			Connection:  flow.connection,
			MapToOperationInput: func(input github.CreateIssueCommentInput) github.CreateIssueCommentInput {
				return input
			},
			Created: sdkgo.GoTo(recordIssueComment{}),
		})),
		dex.DefineStep(recordIssueComment{}),
		dex.DefineStep(github.NewReplaceIssueLabelsStep(github.ReplaceIssueLabelsStepConfig[github.ReplaceIssueLabelsInput]{
			StepType: relabelIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "issue-labels", GroupLabel: "Issue labels", Explanation: "Replace the created issue's labels; a repeat sends the same full set again."},
			Connection:  flow.connection,
			MapToOperationInput: func(input github.ReplaceIssueLabelsInput) github.ReplaceIssueLabelsInput {
				return input
			},
			Replaced: sdkgo.GoTo(recordIssueLabels{}),
		})),
		dex.DefineStep(recordIssueLabels{}),
		dex.DefineStep(github.NewSetIssueStateStep(github.SetIssueStateStepConfig[CloseIssueRequest]{
			StepType: closeIssueStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "issue-closing", GroupLabel: "Issue closing", Explanation: "Close the created issue with the requested reason; a repeat sets the same state again."},
			Connection:  flow.connection,
			MapToOperationInput: func(request CloseIssueRequest) github.SetIssueStateInput {
				return github.SetIssueStateInput{
					Owner: request.Owner, Repository: request.Repository, IssueNumber: request.IssueNumber,
					State: github.IssueStateClosed, StateReason: request.StateReason,
				}
			},
			Updated: sdkgo.GoTo(recordClosedIssue{}),
		})),
		dex.DefineStep(recordClosedIssue{}),
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
	// Rejecting the reason before filing avoids creating an issue the close Step cannot close.
	if !isClosingStateReason(input.CloseWithStateReason) {
		return dex.ForceFail("closeWithStateReason must be completed, not_planned, or empty"), nil
	}
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
// dex:explanation text:"Record the created issue, then complete the filing or start its requested follow-up."
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
	followUp := nextIssueFollowUp(report)
	report.Status = followUp.status
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	switch followUp.kind {
	case issueFollowUpComment:
		return dex.GoTo(sdkgo.StepRef[github.CreateIssueCommentInput](commentOnIssueStepType), issueCommentInput(report)), nil
	case issueFollowUpRelabel:
		return dex.GoTo(sdkgo.StepRef[github.ReplaceIssueLabelsInput](relabelIssueStepType), issueLabelsInput(report)), nil
	case issueFollowUpClose:
		return dex.GoTo(sdkgo.StepRef[CloseIssueRequest](closeIssueStepType), closeIssueRequest(report)), nil
	default:
		return dex.GracefulComplete(report), nil
	}
}

// dex:group group-id:issue-comment group-label:"Issue comment"
// dex:explanation text:"Record the comment, then relabel or close the issue when requested, or complete the filing."
type recordIssueComment struct {
	dex.StepDefaultsNoWaitFor[github.CreateIssueCommentResult]
}

func (recordIssueComment) GetStepType() string { return recordIssueCommentStepType }

func (recordIssueComment) Execute(ctx dex.Context, result github.CreateIssueCommentResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	comment := result.Value
	report.IssueComment = &comment
	followUp := nextIssueFollowUp(report)
	report.Status = followUp.status
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	switch followUp.kind {
	case issueFollowUpRelabel:
		return dex.GoTo(sdkgo.StepRef[github.ReplaceIssueLabelsInput](relabelIssueStepType), issueLabelsInput(report)), nil
	case issueFollowUpClose:
		return dex.GoTo(sdkgo.StepRef[CloseIssueRequest](closeIssueStepType), closeIssueRequest(report)), nil
	default:
		return dex.GracefulComplete(report), nil
	}
}

// dex:group group-id:issue-labels group-label:"Issue labels"
// dex:explanation text:"Record the replaced labels, then close the issue when requested or complete the filing."
type recordIssueLabels struct {
	dex.StepDefaultsNoWaitFor[github.ReplaceIssueLabelsResult]
}

func (recordIssueLabels) GetStepType() string { return recordIssueLabelsStepType }

func (recordIssueLabels) Execute(ctx dex.Context, result github.ReplaceIssueLabelsResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	relabeled := result.Value
	report.RelabeledIssue = &relabeled
	followUp := nextIssueFollowUp(report)
	report.Status = followUp.status
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	if followUp.kind == issueFollowUpClose {
		return dex.GoTo(sdkgo.StepRef[CloseIssueRequest](closeIssueStepType), closeIssueRequest(report)), nil
	}
	return dex.GracefulComplete(report), nil
}

// dex:group group-id:issue-closing group-label:"Issue closing"
// dex:explanation text:"Record the closed issue and complete the filing."
type recordClosedIssue struct {
	dex.StepDefaultsNoWaitFor[github.SetIssueStateResult]
}

func (recordClosedIssue) GetStepType() string { return recordClosedIssueStepType }

func (recordClosedIssue) Execute(ctx dex.Context, result github.SetIssueStateResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	closed := result.Value
	report.ClosedIssue, report.Status = &closed, StatusClosed
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

type issueFollowUpKind uint8

const (
	issueFollowUpNone issueFollowUpKind = iota
	issueFollowUpComment
	issueFollowUpRelabel
	issueFollowUpClose
)

// issueFollowUp is the next requested follow-up on the created issue and the report status it sets.
type issueFollowUp struct {
	kind   issueFollowUpKind
	status Status
}

// nextIssueFollowUp picks the first follow-up still to run, in order: comment, relabel, then close.
func nextIssueFollowUp(report Report) issueFollowUp {
	input := report.Input
	switch {
	case strings.TrimSpace(input.Comment) != "" && report.IssueComment == nil:
		return issueFollowUp{kind: issueFollowUpComment, status: StatusCommentingOnIssue}
	case len(input.RelabelWith) > 0 && report.RelabeledIssue == nil:
		return issueFollowUp{kind: issueFollowUpRelabel, status: StatusRelabelingIssue}
	case input.CloseWithStateReason != "":
		return issueFollowUp{kind: issueFollowUpClose, status: StatusClosingIssue}
	default:
		return issueFollowUp{kind: issueFollowUpNone, status: StatusCreated}
	}
}

func issueCommentInput(report Report) github.CreateIssueCommentInput {
	return github.CreateIssueCommentInput{
		Owner: report.Input.Owner, Repository: report.Input.Repository, IssueNumber: report.CreatedIssue.Number, Body: report.Input.Comment,
	}
}

func issueLabelsInput(report Report) github.ReplaceIssueLabelsInput {
	return github.ReplaceIssueLabelsInput{
		Owner: report.Input.Owner, Repository: report.Input.Repository, IssueNumber: report.CreatedIssue.Number, Labels: report.Input.RelabelWith,
	}
}

func closeIssueRequest(report Report) CloseIssueRequest {
	return CloseIssueRequest{
		Owner: report.Input.Owner, Repository: report.Input.Repository, IssueNumber: report.CreatedIssue.Number,
		StateReason: report.Input.CloseWithStateReason,
	}
}

func openIssuePageRequest(input Input, page int) OpenIssuePageRequest {
	return OpenIssuePageRequest{Owner: input.Owner, Repository: input.Repository, Labels: input.Labels, Page: page}
}

func isClosingStateReason(stateReason string) bool {
	return stateReason == "" || stateReason == github.IssueStateReasonCompleted || stateReason == github.IssueStateReasonNotPlanned
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
