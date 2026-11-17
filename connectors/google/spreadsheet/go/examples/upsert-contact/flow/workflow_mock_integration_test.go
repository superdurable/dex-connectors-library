//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/spreadsheetmock"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestUpsertContactExampleRunsOnSpreadsheetMockWithRealDex(t *testing.T) {
	mock := spreadsheetmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection())
	detailFlow := NewSpreadsheetDetailFlow(mock.Connection())
	operationsFlow := NewSpreadsheetOperationsFlow(mock.Connection())
	registry, err := dex.NewRegistry([]dex.Flow{flow, detailFlow, operationsFlow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	defer func() { require.NoError(t, cache.Close()) }()

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
	defer func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(worker.Stop(stopContext), <-workerResult))
	}()
	{
		registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
		registrationError := worker.WaitPullRegistered(registrationContext)
		cancelRegistration()
		require.NoError(t, registrationError)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	flowID := "google-sheets-upsert-contact-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	request := Input{
		SpreadsheetID: " 1A2B3C4D5E6F7G8H9I0JkLmNoPqRsTuVwXyZ ", SheetName: " Contacts ",
		Email: " ada@example.test ", Name: " Ada Lovelace ", Status: " active ",
	}
	_, err = client.StartFlow(ctx, flow, flowID, request, dex.StartFlowOptions{})
	require.NoError(t, err)
	result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status, result.ErrorMessage)
	var output spreadsheet.UpsertRowOutput
	require.NoError(t, result.DecodeSingleOutput(&output))
	require.Equal(t, spreadsheet.UpsertRowOutput{Action: "updated", RowNumber: 2, UpdatedRange: "Contacts!A2:C2"}, output)
	calls := mock.UpsertRow().Calls()
	require.Len(t, calls, 1)
	require.Equal(t, flowID, calls[0].FlowID)
	detailFlowID := "google-sheets-detail-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	_, err = client.StartFlow(ctx, detailFlow, detailFlowID, spreadsheet.GetSpreadsheetInput{SpreadsheetID: "sample-spreadsheet"}, dex.StartFlowOptions{})
	require.NoError(t, err)
	detailResult, err := client.WaitForFlow(ctx, detailFlowID, dex.WaitForFlowOptions{NeedsResults: true})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, detailResult.Status, detailResult.ErrorMessage)
	var detail spreadsheet.SpreadsheetInfo
	require.NoError(t, detailResult.DecodeSingleOutput(&detail))
	require.Equal(t, "Contacts", detail.Title)
	require.Len(t, mock.GetSpreadsheet().Calls(), 1)
	for _, request := range []SpreadsheetOperationRequest{
		{Operation: "createSheet", SpreadsheetID: "sample-spreadsheet", SheetName: "Contacts"},
		{Operation: "deleteSheet", SpreadsheetID: "sample-spreadsheet", SheetID: 1},
		{Operation: "clearValues", SpreadsheetID: "sample-spreadsheet", Range: "Contacts!A2:C3"},
		{Operation: "appendRow", SpreadsheetID: "sample-spreadsheet", SheetName: "Contacts", Values: []string{"ada@example.test", "Ada", "active"}},
		{Operation: "updateRow", SpreadsheetID: "sample-spreadsheet", SheetName: "Contacts", RowNumber: 2, Values: []string{"ada@example.test", "Ada", "active"}},
	} {
		operationFlowID := "google-sheets-" + request.Operation + "-mock-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		_, err = client.StartFlow(ctx, operationsFlow, operationFlowID, request, dex.StartFlowOptions{})
		require.NoError(t, err)
		operationResult, waitError := client.WaitForFlow(ctx, operationFlowID, dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, waitError)
		require.Equal(t, dex.FlowCompleted, operationResult.Status, operationResult.ErrorMessage)
	}
	require.Len(t, mock.CreateSheet().Calls(), 1)
	require.Len(t, mock.DeleteSheet().Calls(), 1)
	require.Len(t, mock.ClearValues().Calls(), 1)
	require.Len(t, mock.AppendRow().Calls(), 1)
	require.Len(t, mock.UpdateRow().Calls(), 1)
	require.Equal(t, spreadsheet.UpsertRowInput{
		SpreadsheetID: "1A2B3C4D5E6F7G8H9I0JkLmNoPqRsTuVwXyZ", SheetName: "Contacts",
		KeyColumn: "email", KeyValue: "ada@example.test",
		Values: map[string]string{"email": "ada@example.test", "name": "Ada Lovelace", "status": "active"},
	}, calls[0].Input)
}
