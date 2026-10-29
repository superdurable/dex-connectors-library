//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvedrequesttask

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
	"github.com/superdurable/dex-connectors-library/connectors/asana"
	"github.com/superdurable/dex-connectors-library/connectors/asana/asanamock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestApprovedRequestExampleRunsOnAsanaMockWithRealDex(t *testing.T) {
	mock := asanamock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection(), ProjectSelection{ProjectID: "1201000000000001", SectionID: "1201000000000101"})
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{
		FlowServiceAddress: serverAddress,
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	require.NoError(t, worker.WaitPullRegistered(t.Context()))
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "asana-approved-request-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	input := Input{
		RequestID: " REQ-1042 ", Title: " Replace badge reader ", Details: "Door 4 is offline.",
		ApprovedBy: " Grace Hopper ", ApprovalNote: " Budget approved. ",
		AssigneeID: " ada@example.com ", DueOn: "2026-10-15",
	}
	_, err = client.StartFlow(ctx, flow, flowID, input, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var record RequestTask
	require.NoError(t, result.DecodeSingleOutput(&record))
	require.Equal(t, PhaseReady, record.Phase)
	require.Equal(t, "REQ-1042", record.Request.RequestID)
	require.Equal(t, "[REQ-1042] Replace badge reader", record.Request.TaskName)
	require.Equal(t, "Approved by Grace Hopper.\n\nBudget approved.", record.Request.Comment)
	require.Equal(t, 1, record.OpenTaskPagesRead)
	require.Equal(t, 1, record.CreateAttempts)
	require.False(t, record.IsExistingTaskReused)
	require.Equal(t, "1204000000000002", record.TaskID)
	require.Equal(t, "1205000000000001", record.CommentID)

	listCalls := mock.ListTasks().Calls()
	require.Len(t, listCalls, 1)
	require.Equal(t, flowID, listCalls[0].FlowID)
	require.Equal(t, asana.ListTasksInput{ProjectID: "1201000000000001", IsIncompleteOnly: true, PageSize: 100}, listCalls[0].Input)
	createCalls := mock.CreateTask().Calls()
	require.Len(t, createCalls, 1)
	require.Equal(t, asana.CreateTaskInput{
		Name: "[REQ-1042] Replace badge reader", Notes: "Door 4 is offline.",
		ProjectID: "1201000000000001", SectionID: "1201000000000101", DueOn: "2026-10-15",
	}, createCalls[0].Input)
	require.Empty(t, mock.GetTask().Calls())
	updateCalls := mock.UpdateTask().Calls()
	require.Len(t, updateCalls, 1)
	assigneeID, dueOn := "ada@example.com", "2026-10-15"
	require.Equal(t, asana.UpdateTaskInput{
		TaskID: "1204000000000002", SectionID: "1201000000000101", AssigneeID: &assigneeID, DueOn: &dueOn,
	}, updateCalls[0].Input)
	commentCalls := mock.AddComment().Calls()
	require.Len(t, commentCalls, 1)
	require.Equal(t, asana.AddCommentInput{
		TaskID: "1204000000000002", Text: "Approved by Grace Hopper.\n\nBudget approved.",
	}, commentCalls[0].Input)
}
