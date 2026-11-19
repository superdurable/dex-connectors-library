//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datadoglookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/datadog/go"
	"github.com/superdurable/dex-connectors-library/connectors/datadog/go/datadogmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestLookupUsesMockDefaultWithRealDex(t *testing.T) {
	mock := datadogmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	monitorFlow := NewMonitorInspectionFlow(mock.Connection())
	creationFlow := NewMonitorCreationFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, monitorFlow, creationFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
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
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(client.Close(), worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	contextWithDeadline, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request := datadog.QueryMetricsInput{From: 1761436800, To: 1761440400, Query: "system.cpu.user{*}"}
	flowID := "datadog-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(contextWithDeadline, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(contextWithDeadline, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output datadog.MetricsQuery
	require.NoError(t, result.DecodeSingleOutput(&output))
	calls := mock.QueryMetrics().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, request, calls[0].Input)
	require.Equal(t, datadog.QueryMetricsBranchQueried, calls[0].Branch)
	require.Equal(t, flowID, calls[0].FlowID)
	monitorFlowID := "datadog-monitor-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(contextWithDeadline, monitorFlow, monitorFlowID, datadog.GetMonitorInput{MonitorID: 12345}, dex.StartFlowOptions{})
	require.NoError(t, err)
	monitorResult, err := client.WaitForFlow(contextWithDeadline, monitorFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, monitorResult.Status, monitorResult.ErrorMessage)
	var monitor datadog.Monitor
	require.NoError(t, monitorResult.DecodeSingleOutput(&monitor))
	require.Equal(t, int64(12345), monitor.ID)
	require.Len(t, mock.GetMonitor().Calls(), 1)
	creationFlowID := "datadog-monitor-create-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(contextWithDeadline, creationFlow, creationFlowID, datadog.CreateMonitorInput{Name: "Dex latency alert", Query: "avg(last_5m):avg:system.cpu.user{*} > 0.9", Message: "Investigate CPU usage"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	creationResult, err := client.WaitForFlow(contextWithDeadline, creationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, creationResult.Status, creationResult.ErrorMessage)
	var created datadog.Monitor
	require.NoError(t, creationResult.DecodeSingleOutput(&created))
	require.Equal(t, int64(12345), created.ID)
	require.Len(t, mock.CreateMonitor().Calls(), 1)
}
