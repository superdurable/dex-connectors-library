//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuerequest

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear/go"
	"github.com/superdurable/dex-connectors-library/connectors/linear/go/linearmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestIssueRequestExampleRunsOnLinearMockWithRealDex(t *testing.T) {
	const fireDrillIssueID = "44444444-4444-4444-8444-444444444444"
	mock := linearmock.New(t, ConnectionName)
	mock.SearchIssues().Respond(linearmock.SearchIssuesNoOpenFireDrillIssue())
	flow := NewFlow(mock.Connection(), TeamSelection{TeamID: integrationTeamID, TeamKey: "ENG", TeamName: "Engineering"})
	harness := &requestHarness{flow: flow, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	harness.startWorker(t)

	input := integrationInput()
	flowID := harness.startIssueRequest(t, "mock-default", input)
	result := harness.waitForFlow(t, flowID)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome IssueRequestOutcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Equal(t, IssueCreated, outcome.Action)
	require.Equal(t, fireDrillIssueID, outcome.IssueID)
	require.Equal(t, aliceUserID, outcome.AssigneeID)
	require.Equal(t, "In Progress", outcome.StateName)
	require.Equal(t, linear.WorkflowStateTypeStarted, outcome.ReadBackStateType)
	require.False(t, outcome.WasCreateReplayed)

	users := mock.FindUserByEmail().Calls()
	require.Len(t, users, 1)
	require.Equal(t, flowID, users[0].FlowID)
	require.Equal(t, linear.FindUserByEmailInput{Email: input.AssigneeEmail}, users[0].Input)
	searches := mock.SearchIssues().Calls()
	require.Len(t, searches, 1)
	require.Equal(t, linear.SearchIssuesInput{
		Filter:   linear.IssueSearchFilter{TeamID: integrationTeamID, Title: input.Title, StateTypes: []linear.WorkflowStateType{"triage", "backlog", "unstarted", "started"}},
		PageSize: 10,
	}, searches[0].Input)
	creates := mock.CreateIssue().Calls()
	require.Len(t, creates, 1)
	require.Equal(t, linear.CreateIssueInput{
		TeamID: integrationTeamID, Title: input.Title, Description: input.Description, AssigneeID: aliceUserID,
	}, creates[0].Input)
	states := mock.ListWorkflowStates().Calls()
	require.Len(t, states, 1)
	require.Equal(t, linear.ListWorkflowStatesInput{TeamID: integrationTeamID}, states[0].Input)
	updates := mock.UpdateIssue().Calls()
	require.Len(t, updates, 1)
	require.Equal(t, linear.UpdateIssueInput{
		IssueID: fireDrillIssueID, StateID: inProgressStateID, AssigneeID: aliceUserID,
	}, updates[0].Input)
	comments := mock.AddComment().Calls()
	require.Len(t, comments, 1)
	require.Equal(t, linear.AddCommentInput{IssueID: fireDrillIssueID, Body: input.Comment}, comments[0].Input)
	reads := mock.GetIssue().Calls()
	require.Len(t, reads, 1)
	require.Equal(t, linear.GetIssueInput{IssueID: fireDrillIssueID}, reads[0].Input)
}
