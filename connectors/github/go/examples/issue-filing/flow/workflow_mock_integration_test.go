//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuefiling

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/github/go/githubmock"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestIssueFilingExampleRunsOnTheGitHubMockWithRealDex runs the example Flow on a real Worker with the
// generated githubmock connection, whose issue listing default serves three pages.
func TestIssueFilingExampleRunsOnTheGitHubMockWithRealDex(t *testing.T) {
	mock := githubmock.New(t, ConnectionName)
	flow, harness := newIssueFilingHarnessWithConnection(t, mock.Connection())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)

	createdFlowID := "github-issue-filing-mock-created-" + testRunID
	input := Input{Owner: "octocat", Repository: "hello-world", Title: "Export fails for large projects", Labels: []string{"bug"}}
	_, err := harness.client.StartFlow(ctx, flow, createdFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	report := completedReport(t, ctx, harness.client, createdFlowID)
	require.Equal(t, StatusCreated, report.Status)
	require.Equal(t, 3, report.OpenIssuePagesRead, "the example follows nextPage through every mock page")
	require.False(t, report.HasUncheckedOpenIssues)
	require.Equal(t, 42, report.CreatedIssue.Number)

	trackedFlowID := "github-issue-filing-mock-tracked-" + testRunID
	input.Title = "crash when saving an empty draft"
	_, err = harness.client.StartFlow(ctx, flow, trackedFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	report = completedReport(t, ctx, harness.client, trackedFlowID)
	require.Equal(t, StatusAlreadyTracked, report.Status)
	require.Equal(t, 12, report.ExistingIssue.Number)

	closedFlowID := "github-issue-filing-mock-closed-" + testRunID
	input.Title, input.CloseWithStateReason = "Export fails for large projects", "completed"
	_, err = harness.client.StartFlow(ctx, flow, closedFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	report = completedReport(t, ctx, harness.client, closedFlowID)
	require.Equal(t, StatusClosed, report.Status)
	require.Equal(t, 42, report.ClosedIssue.Number)
	require.Equal(t, "completed", report.ClosedIssue.StateReason)
	input.CloseWithStateReason = ""

	followUpFlowID := "github-issue-filing-mock-follow-up-" + testRunID
	input.Comment, input.RelabelWith = "Reproduced on the latest release.", []string{"bug", "triaged"}
	_, err = harness.client.StartFlow(ctx, flow, followUpFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	report = completedReport(t, ctx, harness.client, followUpFlowID)
	require.Equal(t, StatusCreated, report.Status)
	require.Equal(t, int64(1987654321), report.IssueComment.CommentID)
	require.Equal(t, []string{"bug", "triaged"}, report.RelabeledIssue.Labels)
	input.Comment, input.RelabelWith = "", nil

	uncertainFlowID := "github-issue-filing-mock-uncertain-" + testRunID
	mock.CreateIssue().ForFlow(uncertainFlowID).Respond(githubmock.CreateIssueCreateOutcomeUnknown())
	input.Title = "Export fails for large projects"
	_, err = harness.client.StartFlow(ctx, flow, uncertainFlowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result := waitForTerminalFlow(t, ctx, harness.client, uncertainFlowID)
	require.Equal(t, dex.FlowFailed, result.Status, "an unwired uncertain branch fails the Flow for reconciliation")

	creates := 0
	for _, call := range mock.CreateIssue().Calls() {
		if call.FlowID == trackedFlowID {
			creates++
		}
	}
	require.Zero(t, creates, "an already tracked title creates nothing")
}
