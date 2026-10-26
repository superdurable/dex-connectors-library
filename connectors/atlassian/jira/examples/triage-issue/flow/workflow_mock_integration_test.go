//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package triageissue

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/jiramock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTriageIssueUsesJiraMockDefaultsWithRealDex(t *testing.T) {
	mock := jiramock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection(), ProjectSelection{})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	workerAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress, WorkerTarget: &dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{
		BindAddress: workerAddress, FlowServiceAddress: serverAddress,
		WorkerTarget: dex.WorkerTarget{Address: workerAddress},
	})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "jira-triage-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{ProjectKey: "OPS", Summary: "Loading dock light out", TriageComment: "Paging the on-call electrician.", DestinationStatusName: "In Progress", Labels: []string{"facilities"}}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var triage IssueTriage
	require.NoError(t, result.DecodeSingleOutput(&triage))
	require.Equal(t, PhaseTriaged, triage.Phase)
	require.Equal(t, input.Summary, triage.Issue.Summary)
	require.Equal(t, input.ProjectKey, triage.Issue.Project.Key)
	require.Equal(t, "OPS-4", triage.Issue.Key)
	require.Equal(t, "10501", triage.CommentID)
	require.Equal(t, input.DestinationStatusName, triage.Status.Name)
	require.False(t, triage.IsExistingIssueReused)

	searchCalls := mock.SearchIssues().Calls()
	require.Len(t, searchCalls, 1)
	require.Equal(t, flowID, searchCalls[0].FlowID)
	require.Equal(t, jira.SearchIssuesInput{Filter: &jira.IssueSearchFilter{
		ProjectKeys: []string{"OPS"}, StatusCategories: []jira.StatusCategoryKey{jira.StatusCategoryToDo, jira.StatusCategoryInProgress}, SummaryPhrase: "Loading dock light out",
	}, PageSize: 20}, searchCalls[0].Input)
	createCalls := mock.CreateIssue().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, jira.CreateIssueInput{ProjectKey: "OPS", IssueTypeName: "Task", Summary: "Loading dock light out", Labels: []string{"facilities"}}, createCalls[0].Input)
	getCalls := mock.GetIssue().Calls()
	require.Len(t, getCalls, 1)
	require.Equal(t, jira.GetIssueInput{IssueIDOrKey: "OPS-4"}, getCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 1)
	require.Equal(t, jira.AddCommentInput{IssueIDOrKey: "OPS-4", Body: "Paging the on-call electrician."}, commentCalls[0].Input)
	transitionCalls := mock.TransitionIssue().Calls()
	require.Len(t, transitionCalls, 1)
	require.Equal(t, jira.TransitionIssueInput{IssueIDOrKey: "OPS-4", DestinationStatusName: "In Progress"}, transitionCalls[0].Input)
}
