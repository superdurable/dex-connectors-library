//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package flow

import (
	"context"
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go"
	listinspection "github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/examples/task-inspection/flow/lists"
	taskinspection "github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/examples/task-inspection/flow/tasks"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/todo/go/todomock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestTaskInspectionUsesBothMocksWithRealDex(t *testing.T) {
	mock := todomock.New(t, listinspection.ConnectionName)
	listFlow, taskFlow := listinspection.NewListFlow(mock.Connection()), taskinspection.NewTaskFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{listFlow, taskFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	nonce := strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, listFlow, "microsoft-todo-lists-"+nonce, todo.ListTaskListsInput{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	lists, err := client.WaitForFlow(ctx, "microsoft-todo-lists-"+nonce, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, lists.Status, lists.ErrorMessage)
	var listOutput todo.TaskListsPage
	require.NoError(t, lists.DecodeSingleOutput(&listOutput))
	require.Equal(t, "sample-list-1", listOutput.Lists[0].ID)
	_, err = client.StartFlow(ctx, taskFlow, "microsoft-todo-tasks-"+nonce, todo.ListTasksInput{ListID: listOutput.Lists[0].ID}, dex.StartFlowOptions{})
	require.NoError(t, err)
	tasks, err := client.WaitForFlow(ctx, "microsoft-todo-tasks-"+nonce, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, tasks.Status, tasks.ErrorMessage)
	var taskOutput todo.TasksPage
	require.NoError(t, tasks.DecodeSingleOutput(&taskOutput))
	require.Equal(t, "sample-task-1", taskOutput.Tasks[0].ID)
	require.Len(t, mock.ListTaskLists().Calls(), 1)
	require.Len(t, mock.ListTasks().Calls(), 1)
}
