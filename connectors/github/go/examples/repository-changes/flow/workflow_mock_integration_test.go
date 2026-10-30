//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package repositorychanges

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/githubmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestRepositoryChangesExampleRunsOnTheGitHubMockWithRealDex runs the example Flow on a real Worker with the
// generated githubmock connection, whose paged defaults serve three pages per listing.
func TestRepositoryChangesExampleRunsOnTheGitHubMockWithRealDex(t *testing.T) {
	mock := githubmock.New(t, ConnectionName)
	flow, harness := newRepositoryChangesHarnessWithConnection(t, mock.Connection())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{Owner: "octocat", Repository: "hello-world", WindowStart: integrationWindowStart, WindowEnd: integrationWindowEnd}

	reportedFlowID := "github-repository-changes-mock-" + testRunID
	_, err := harness.client.StartFlow(ctx, flow, reportedFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, reportedFlowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var report Report
	require.NoError(t, result.DecodeSingleOutput(&report))
	require.Equal(t, StatusCompleted, report.Status)
	require.Equal(t, 5, report.MergedPullRequestCount)
	require.True(t, report.MorePullRequests, "the example reads one bounded page and reports that more follow")
	require.Equal(t, []int{42, 40}, []int{report.PullRequests[0].PullRequest.Number, report.PullRequests[1].PullRequest.Number})
	require.True(t, report.PullRequests[0].FilesLoaded)
	require.True(t, report.PullRequests[0].MoreFiles)
	require.Len(t, report.Commits, 2)
	require.True(t, report.MoreCommits)

	rejectedFlowID := "github-repository-changes-mock-rejected-" + testRunID
	mock.ListMergedPullRequests().ForFlow(rejectedFlowID).Respond(githubmock.ListMergedPullRequestsProviderRejectedFailure())
	_, err = harness.client.StartFlow(ctx, flow, rejectedFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowFailed, waitForTerminalFlow(t, ctx, harness.client, rejectedFlowID).Status)

	commitReads := 0
	for _, call := range mock.ListCommits().Calls() {
		if call.FlowID == rejectedFlowID {
			commitReads++
		}
	}
	require.Zero(t, commitReads, "a rejected search stops the report before the commit read")
}
