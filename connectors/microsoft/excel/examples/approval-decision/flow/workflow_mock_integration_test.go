//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package approvaldecision

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/excelmock"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestApprovalDecisionExampleRunsOnExcelMockWithRealDex(t *testing.T) {
	mock := excelmock.New(t, ConnectionName)
	flow := NewFlow(mock.Connection(),
		sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: PolicyTableConfigurationRef(), Value: TableConfiguration{
			DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: policyTableID, TableName: "ApprovalPolicy"}},
		sdkgo.ConnectorLoadedConfiguration[TableConfiguration]{Reference: DecisionTableConfigurationRef(), Value: TableConfiguration{
			DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: decisionTableID, TableName: "Decisions"}},
		sdkgo.ConnectorLoadedConfiguration[WorksheetConfiguration]{Reference: SummaryWorksheetConfigurationRef(), Value: WorksheetConfiguration{
			DriveID: integrationDriveID, WorkbookID: summaryWorkbookID, Worksheet: summaryWorksheetID, WorksheetName: "Summary"}},
	)
	harness := &approvalHarness{flow: flow, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801")}
	var err error
	harness.registry, err = dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	harness.cache, err = blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	harness.workerAddress = net.JoinHostPort("127.0.0.1", availableIntegrationPort(t))
	harness.client, err = dex.NewClient(harness.registry, harness.cache, dex.ClientOptions{
		FlowServiceAddress: harness.serverAddress, WorkerTarget: &dex.WorkerTarget{Address: harness.workerAddress},
	})
	require.NoError(t, err)
	harness.startWorker(t)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})

	input := travelRequest("REQ-1001", 420)
	decision := harness.runDecision(t, "mock-default", input)
	require.Equal(t, PhaseCompleted, decision.Phase)
	require.Equal(t, DecisionApproved, decision.Decision)
	require.True(t, decision.IsLogRowAppended)
	require.False(t, decision.WasLogRowAlreadyPresent)
	require.True(t, decision.IsSummaryConfirmed)

	policyReads := mock.GetTableRows().Calls()
	require.Len(t, policyReads, 1)
	require.Equal(t, excel.GetTableRowsInput{DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: policyTableID}, policyReads[0].Input)
	appends := mock.AppendTableRows().Calls()
	require.Len(t, appends, 1)
	require.Equal(t, excel.AppendTableRowsInput{
		DriveID: integrationDriveID, WorkbookID: policyWorkbookID, Table: decisionTableID, KeyColumn: "RequestId",
		Rows: []map[string]excel.CellValue{{
			"RequestId": excel.TextCellValue(input.RequestID),
			"Requester": excel.TextCellValue(input.Requester),
			"Category":  excel.TextCellValue(input.Category),
			"AmountUsd": excel.NumberCellValue(input.AmountUSD),
			"Decision":  excel.TextCellValue(string(DecisionApproved)),
			"Approver":  excel.EmptyCellValue(),
			"DecidedAt": excel.TextCellValue(decision.DecidedAt),
		}},
	}, appends[0].Input)
	writes := mock.UpdateValues().Calls()
	require.Len(t, writes, 1)
	require.Equal(t, excel.UpdateValuesInput{
		DriveID: integrationDriveID, WorkbookID: summaryWorkbookID, Worksheet: summaryWorksheetID, Address: "A2:E2",
		Values: [][]excel.CellValue{{
			excel.TextCellValue(input.RequestID), excel.TextCellValue(input.Category), excel.NumberCellValue(input.AmountUSD),
			excel.TextCellValue(string(DecisionApproved)), excel.TextCellValue(decision.DecidedAt),
		}},
	}, writes[0].Input)
	readBacks := mock.GetValues().Calls()
	require.Len(t, readBacks, 1)
	require.Equal(t, excel.GetValuesInput{
		DriveID: integrationDriveID, WorkbookID: summaryWorkbookID, Worksheet: summaryWorksheetID, Address: "A2:E2",
	}, readBacks[0].Input)
}
