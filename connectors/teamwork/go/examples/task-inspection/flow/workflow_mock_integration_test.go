//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go"
	"github.com/superdurable/dex-connectors-library/connectors/teamwork/go/teamworkmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestTaskInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := teamworkmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	createFlow := NewCreateTaskFlow(mock.Connection())
	projectFlow := NewGetProjectFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, createFlow, projectFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	serverAddress := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if serverAddress == "" {
		serverAddress = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: serverAddress})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopCtx), <-workerResult, client.Close(), cache.Close()))
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	flowID := "teamwork-taskinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, teamwork.GetTaskInput{TaskID: "12345"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output teamwork.TaskSummary
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "12345", output.ID)
	require.Equal(t, "Review launch", output.Name)
	require.Equal(t, "pending", output.Status)
	require.Len(t, mock.GetTask().Calls(), 1)
	createFlowID := "teamwork-create-task-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, createFlow, createFlowID, teamwork.CreateTaskInput{TasklistID: "321", Name: "Review launch"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	createdResult, err := client.WaitForFlow(ctx, createFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, createdResult.Status, createdResult.ErrorMessage)
	var createdTask teamwork.TaskSummary
	require.NoError(t, createdResult.DecodeSingleOutput(&createdTask))
	require.Equal(t, "12345", createdTask.ID)
	require.Len(t, mock.CreateTask().Calls(), 1)
	projectFlowID := "teamwork-project-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, projectFlow, projectFlowID, teamwork.GetProjectInput{ProjectID: "123"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	projectResult, err := client.WaitForFlow(ctx, projectFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, projectResult.Status, projectResult.ErrorMessage)
	var project teamwork.ProjectSummary
	require.NoError(t, projectResult.DecodeSingleOutput(&project))
	require.Equal(t, "123", project.ID)
	require.Len(t, mock.GetProject().Calls(), 1)
}
