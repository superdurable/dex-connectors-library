//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package job_runinspection

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/databricks/go"
	"github.com/superdurable/dex-connectors-library/connectors/databricks/go/databricksmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestJobRunInspectionUsesDefaultMockWithRealDex(t *testing.T) {
	mock := databricksmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	listingFlow := NewJobListingFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, listingFlow})
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
	flowID := "databricks-job_runinspection-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, databricks.GetJobRunInput{RunID: "455644833"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output databricks.JobRunStatus
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, "455644833", output.ID)
	require.Equal(t, "", output.ResultState)
	require.Equal(t, "RUNNING", output.Status)
	require.Len(t, mock.GetJobRun().Calls(), 1)
	listingFlowID := "databricks-job-listing-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, listingFlow, listingFlowID, databricks.ListJobsInput{Name: "Example job"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	listingResult, err := client.WaitForFlow(ctx, listingFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, listingResult.Status, listingResult.ErrorMessage)
	var jobPage databricks.JobPage
	require.NoError(t, listingResult.DecodeSingleOutput(&jobPage))
	require.Equal(t, []databricks.JobSummary{{ID: "11223344", Name: "Example job"}}, jobPage.Jobs)
	require.Len(t, mock.ListJobs().Calls(), 1)
}
