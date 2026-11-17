//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheetlookup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go"
	"github.com/superdurable/dex-connectors-library/connectors/smartsheet/go/smartsheetmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMockAnswersWithRealDex(t *testing.T) {
	mock := smartsheetmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	server := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if server == "" {
		server = "127.0.0.1:8801"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "smartsheet-lookup-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, Request{}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var outcome Outcome
	require.NoError(t, result.DecodeSingleOutput(&outcome))
	require.Len(t, mock.ListSheets().Calls(), 1)
	require.Len(t, mock.GetSheet().Calls(), 1)
}

func TestRowLookupMockWithRealDex(t *testing.T) {
	mock := smartsheetmock.New(t, ConnectionName)
	flow := NewRowLookupFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	server := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if server == "" {
		server = "127.0.0.1:9580"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	flowID := "smartsheet-row-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, smartsheet.GetRowInput{SheetID: 1001, RowID: 3001}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var row smartsheet.SheetRow
	require.NoError(t, result.DecodeSingleOutput(&row))
	require.Equal(t, int64(3001), row.ID)
	require.Len(t, mock.GetRow().Calls(), 1)
}

func TestSheetCreateMockWithRealDex(t *testing.T) {
	mock := smartsheetmock.New(t, ConnectionName)
	flow := NewSheetCreateFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	server := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if server == "" {
		server = "127.0.0.1:9580"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	flowID := "smartsheet-sheet-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, smartsheet.CreateSheetInWorkspaceInput{WorkspaceID: 4001, Name: "Meridian Planning", PrimaryColumnTitle: "Task"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var sheet smartsheet.SheetSummary
	require.NoError(t, result.DecodeSingleOutput(&sheet))
	require.Equal(t, int64(1001), sheet.ID)
	require.Len(t, mock.CreateSheetInWorkspace().Calls(), 1)
}
func TestColumnLookupMockWithRealDex(t *testing.T) {
	mock := smartsheetmock.New(t, ConnectionName)
	flow := NewColumnLookupFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()
	server := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if server == "" {
		server = "127.0.0.1:9580"
	}
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	workerResult := make(chan error, 1)
	go func() { workerResult <- worker.Start() }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(ctx), <-workerResult))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(ctx))
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: server})
	require.NoError(t, err)
	defer func() { require.NoError(t, client.Close()) }()
	flowID := "smartsheet-column-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, flow, flowID, smartsheet.GetColumnInput{SheetID: 1001, ColumnID: 2001}, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var column smartsheet.ColumnSummary
	require.NoError(t, result.DecodeSingleOutput(&column))
	require.Equal(t, int64(2001), column.ID)
	require.Len(t, mock.GetColumn().Calls(), 1)
}
