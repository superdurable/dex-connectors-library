// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositorychanges

import (
	"testing"

	githubconnector "github.com/superdurable/dex-connectors-library/connectors/github"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRegisteredTypesMatchTheFlowDefinitionGraph guards Dex Web Start Flow, which starts the graph's
// unqualified Flow and start Step names.
func TestRegisteredTypesMatchTheFlowDefinitionGraph(t *testing.T) {
	if flowType := dex.GetFinalFlowType(&Flow{}); flowType != "GitHubRepositoryChanges" {
		t.Fatalf("Flow type = %q", flowType)
	}
	for got, want := range map[string]string{
		dex.GetFinalStepType[Input](startRepositoryChangesReport{}):                                    "startRepositoryChangesReport",
		dex.GetFinalStepType[githubconnector.ListMergedPullRequestsResult](recordMergedPullRequests{}): "recordMergedPullRequests",
		dex.GetFinalStepType[githubconnector.ListPullRequestFilesResult](recordPullRequestFiles{}):     "recordPullRequestFiles",
		dex.GetFinalStepType[githubconnector.ListCommitsResult](completeRepositoryChangesReport{}):     "completeRepositoryChangesReport",
	} {
		if got != want {
			t.Fatalf("Step type = %q, want %q", got, want)
		}
	}
}

func TestFileReadsFollowTheNewestBoundedPullRequests(t *testing.T) {
	report := Report{Input: Input{Owner: "octocat", Repository: "hello-world"}}
	for _, number := range []int{44, 43, 42, 41} {
		report.PullRequests = append(report.PullRequests, PullRequestChanges{PullRequest: githubconnector.MergedPullRequest{Number: number}})
	}
	for _, want := range []int{44, 43, 42} {
		request, hasPendingRequest := nextPullRequestFilesRequest(report)
		if !hasPendingRequest || request != (PullRequestFilesRequest{Owner: "octocat", Repository: "hello-world", Number: want}) {
			t.Fatalf("next request = %+v, pending %v; want pull request %d", request, hasPendingRequest, want)
		}
		index, _ := nextPullRequestWithoutFiles(report)
		report.PullRequests[index].FilesLoaded = true
	}
	if request, hasPendingRequest := nextPullRequestFilesRequest(report); hasPendingRequest {
		t.Fatalf("pull request %d is beyond the %d-pull-request file bound", request.Number, PullRequestsWithFiles)
	}
	if _, hasPendingRequest := nextPullRequestFilesRequest(Report{}); hasPendingRequest {
		t.Fatal("a report without merged pull requests has no file reads")
	}
}
