// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package repositorychanges demonstrates the GitHub repository change Query APIs.
package repositorychanges

import (
	"errors"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow type shared by Dex Web Start Flow, the Worker registry, and open runs.
	FlowType                       = "GitHubRepositoryChanges"
	ConnectionName                 = "github-repository-changes"
	listMergedPullRequestsStepType = "ListMergedPullRequests"
	listPullRequestFilesStepType   = "ListPullRequestFiles"
	listCommitsStepType            = "ListCommits"
	// Application Step types match the Flow Definition Graph so Dex Web starts and displays the same Steps.
	startRepositoryChangesReportStepType    = "startRepositoryChangesReport"
	recordMergedPullRequestsStepType        = "recordMergedPullRequests"
	recordPullRequestFilesStepType          = "recordPullRequestFiles"
	completeRepositoryChangesReportStepType = "completeRepositoryChangesReport"
	// PullRequestPageSize bounds the merged pull requests read for one report.
	PullRequestPageSize = 10
	// PullRequestsWithFiles bounds how many of the newest merged pull requests have their files read.
	PullRequestsWithFiles = 3
	// FilePageSize bounds the files read for one pull request.
	FilePageSize = 20
	// CommitPageSize bounds the commits read for one report.
	CommitPageSize = 20
)

var reportAttribute = dex.DefineAttribute[Report]("github-repository-changes-report")

type Status string

const (
	StatusListingPullRequests Status = "listingPullRequests"
	StatusListingFiles        Status = "listingFiles"
	StatusListingCommits      Status = "listingCommits"
	StatusCompleted           Status = "completed"
)

// Input names one public repository and an inclusive time window.
type Input struct {
	Owner       string    `json:"owner"`
	Repository  string    `json:"repository"`
	WindowStart time.Time `json:"windowStart"`
	WindowEnd   time.Time `json:"windowEnd"`
}

// PullRequestFilesRequest is the durable input of one changed-file read.
type PullRequestFilesRequest struct {
	Owner      string `json:"owner"`
	Repository string `json:"repository"`
	Number     int    `json:"number"`
}

// Report is the bounded record of repository changes that the Flow returns.
type Report struct {
	Input                  Input                  `json:"input"`
	Status                 Status                 `json:"status"`
	MergedPullRequestCount int                    `json:"mergedPullRequestCount"`
	MorePullRequests       bool                   `json:"morePullRequests"`
	PullRequests           []PullRequestChanges   `json:"pullRequests"`
	Commits                []github.CommitSummary `json:"commits"`
	MoreCommits            bool                   `json:"moreCommits"`
}

type PullRequestChanges struct {
	PullRequest github.MergedPullRequest `json:"pullRequest"`
	FilesLoaded bool                     `json:"filesLoaded"`
	Files       []github.PullRequestFile `json:"files,omitempty"`
	MoreFiles   bool                     `json:"moreFiles"`
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
		dex.DefineStartStep(startRepositoryChangesReport{}),
		dex.DefineStep(github.NewListMergedPullRequestsStep(github.ListMergedPullRequestsStepConfig[Input]{
			StepType: listMergedPullRequestsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "github", GroupLabel: "GitHub", Explanation: "List the newest pull requests merged in the report window."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) github.ListMergedPullRequestsInput {
				return github.ListMergedPullRequestsInput{
					Owner: input.Owner, Repository: input.Repository,
					MergedAfter: input.WindowStart, MergedBefore: input.WindowEnd, PageSize: PullRequestPageSize,
				}
			},
			Listed: sdkgo.GoTo(recordMergedPullRequests{}),
		})),
		dex.DefineStep(recordMergedPullRequests{}),
		dex.DefineStep(github.NewListPullRequestFilesStep(github.ListPullRequestFilesStepConfig[PullRequestFilesRequest]{
			StepType: listPullRequestFilesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "github", GroupLabel: "GitHub", Explanation: "List the files changed by one merged pull request."},
			Connection:  flow.connection,
			MapToOperationInput: func(request PullRequestFilesRequest) github.ListPullRequestFilesInput {
				return github.ListPullRequestFilesInput{
					Owner: request.Owner, Repository: request.Repository, Number: request.Number, PageSize: FilePageSize,
				}
			},
			Listed: sdkgo.GoTo(recordPullRequestFiles{}),
		})),
		dex.DefineStep(recordPullRequestFiles{}),
		dex.DefineStep(github.NewListCommitsStep(github.ListCommitsStepConfig[Input]{
			StepType: listCommitsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "github", GroupLabel: "GitHub", Explanation: "List the newest default-branch commits in the report window."},
			Connection:  flow.connection,
			MapToOperationInput: func(input Input) github.ListCommitsInput {
				return github.ListCommitsInput{
					Owner: input.Owner, Repository: input.Repository,
					Since: input.WindowStart, Until: input.WindowEnd, PageSize: CommitPageSize,
				}
			},
			Listed: sdkgo.GoTo(completeRepositoryChangesReport{}),
		})),
		dex.DefineStep(completeRepositoryChangesReport{}),
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

// dex:field attribute-key:github-repository-changes-report value-type:json editable:false description:"GitHub repository change status"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-repository-changes-report": report}}, nil
}

// dex:field attribute-key:github-repository-changes-report value-type:json editable:false description:"GitHub repository change report"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := optionalReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"github-repository-changes-report": report}}, nil
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

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record the repository and window before reading GitHub."
type startRepositoryChangesReport struct {
	dex.StepDefaults
}

func (startRepositoryChangesReport) GetStepType() string { return startRepositoryChangesReportStepType }

// WaitFor skips at once; Dex Web Start Flow (Dex CLI v0.13.8) calls it, and execute-only start Steps
// reject that.
func (startRepositoryChangesReport) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (startRepositoryChangesReport) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if err := reportAttribute.Set(ctx, Report{Input: input, Status: StatusListingPullRequests}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](listMergedPullRequestsStepType), input), nil
}

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record the merged pull requests, then read files for the newest ones."
type recordMergedPullRequests struct {
	dex.StepDefaultsNoWaitFor[github.ListMergedPullRequestsResult]
}

func (recordMergedPullRequests) GetStepType() string { return recordMergedPullRequestsStepType }

func (recordMergedPullRequests) Execute(ctx dex.Context, result github.ListMergedPullRequestsResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.MergedPullRequestCount = result.Value.TotalCount
	report.MorePullRequests = result.Value.NextPage != 0
	report.PullRequests = make([]PullRequestChanges, 0, len(result.Value.PullRequests))
	for _, pullRequest := range result.Value.PullRequests {
		report.PullRequests = append(report.PullRequests, PullRequestChanges{PullRequest: pullRequest})
	}
	if request, hasPendingRequest := nextPullRequestFilesRequest(report); hasPendingRequest {
		report.Status = StatusListingFiles
		if err := reportAttribute.Set(ctx, report); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[PullRequestFilesRequest](listPullRequestFilesStepType), request), nil
	}
	report.Status = StatusListingCommits
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](listCommitsStepType), report.Input), nil
}

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record one pull request's files, then read the next pull request or the commits."
type recordPullRequestFiles struct {
	dex.StepDefaultsNoWaitFor[github.ListPullRequestFilesResult]
}

func (recordPullRequestFiles) GetStepType() string { return recordPullRequestFilesStepType }

func (recordPullRequestFiles) Execute(ctx dex.Context, result github.ListPullRequestFilesResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	index, hasPendingPullRequest := nextPullRequestWithoutFiles(report)
	if !hasPendingPullRequest {
		return nil, errors.New("GitHub changed files arrived without a pending pull request")
	}
	report.PullRequests[index].FilesLoaded = true
	report.PullRequests[index].Files = result.Value.Files
	report.PullRequests[index].MoreFiles = result.Value.NextPage != 0
	if request, hasPendingRequest := nextPullRequestFilesRequest(report); hasPendingRequest {
		if err := reportAttribute.Set(ctx, report); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[PullRequestFilesRequest](listPullRequestFilesStepType), request), nil
	}
	report.Status = StatusListingCommits
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](listCommitsStepType), report.Input), nil
}

// dex:group group-id:report group-label:"Report"
// dex:explanation text:"Record the commits and complete the repository change report."
type completeRepositoryChangesReport struct {
	dex.StepDefaultsNoWaitFor[github.ListCommitsResult]
}

func (completeRepositoryChangesReport) GetStepType() string {
	return completeRepositoryChangesReportStepType
}

func (completeRepositoryChangesReport) Execute(ctx dex.Context, result github.ListCommitsResult) (*dex.StepDecision, error) {
	report, err := reportAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	report.Commits = result.Value.Commits
	report.MoreCommits = result.Value.NextPage != 0
	report.Status = StatusCompleted
	if err := reportAttribute.Set(ctx, report); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(report), nil
}

// nextPullRequestWithoutFiles returns the newest bounded pull request whose files are not recorded yet.
func nextPullRequestWithoutFiles(report Report) (int, bool) {
	for index := 0; index < len(report.PullRequests) && index < PullRequestsWithFiles; index++ {
		if !report.PullRequests[index].FilesLoaded {
			return index, true
		}
	}
	return 0, false
}

func nextPullRequestFilesRequest(report Report) (PullRequestFilesRequest, bool) {
	index, hasPendingPullRequest := nextPullRequestWithoutFiles(report)
	if !hasPendingPullRequest {
		return PullRequestFilesRequest{}, false
	}
	return PullRequestFilesRequest{
		Owner: report.Input.Owner, Repository: report.Input.Repository, Number: report.PullRequests[index].PullRequest.Number,
	}, true
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
