// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const decisionsTableID = "{6D182180-0000-4000-8000-000000000002}"

func newDecisionsProvider(t *testing.T, rows ...[]any) *fakeexcel.Server {
	t.Helper()
	provider := fakeexcel.New(t, excelTestToken, "refresh-SENTINEL")
	provider.AddTable(testDriveID, testWorkbookID, fakeexcel.Table{
		ID: decisionsTableID, Name: "Decisions", Columns: []string{"RequestId", "Decision", "AmountUsd"}, Rows: rows,
	})
	return provider
}

func decisionRow(requestID string, decision string, amount float64) map[string]excel.CellValue {
	return map[string]excel.CellValue{
		"RequestId": excel.TextCellValue(requestID), "Decision": excel.TextCellValue(decision), "AmountUsd": excel.NumberCellValue(amount),
	}
}

func appendDecisionsInput(rows ...map[string]excel.CellValue) excel.AppendTableRowsInput {
	return excel.AppendTableRowsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: decisionsTableID, KeyColumn: "RequestId", Rows: rows}
}

func TestAppendTableRowsAppendsOnlyMissingKeysInColumnOrder(t *testing.T) {
	provider := newDecisionsProvider(t, []any{"REQ-1", "approved", 10.0})
	ctx := newDexContext("append")
	input := appendDecisionsInput(decisionRow("REQ-1", "approved", 10), decisionRow("00123", "escalated", 900),
		map[string]excel.CellValue{"RequestId": excel.TextCellValue("REQ-3")})

	result, err := sdkgo.RunMutation(ctx, newExcelClient(t, provider.URL).AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
	require.Equal(t, []excel.CellValue{excel.TextCellValue("00123"), excel.TextCellValue("REQ-3")}, result.Value.AppendedKeys)
	require.Equal(t, []excel.CellValue{excel.TextCellValue("REQ-1")}, result.Value.AlreadyPresentKeys)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, [][]any{{"REQ-1", "approved", 10.0}, {"00123", "escalated", 900.0}, {"REQ-3", "", ""}},
		provider.TableRows(testDriveID, testWorkbookID, "Decisions"), "00123 stays text and the omitted columns are empty")
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.NotNil(t, ctx.recordedHeartbeat, "the dispatch checkpoint is recorded before the append")
}

func TestAppendTableRowsWithEveryKeyPresentSendsNothing(t *testing.T) {
	provider := newDecisionsProvider(t, []any{"REQ-1", "approved", 10.0}, []any{"'REQ-2", "approved", 20.0})
	ctx := newDexContext("present")
	result, err := sdkgo.RunMutation(ctx, newExcelClient(t, provider.URL).AppendTableRows(), excelConnection,
		appendDecisionsInput(decisionRow("REQ-1", "approved", 10), decisionRow("REQ-2", "approved", 20)))
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
	require.Empty(t, result.Value.AppendedKeys)
	require.Len(t, result.Value.AlreadyPresentKeys, 2, "a key stored with Excel's text prefix still matches")
	require.Zero(t, provider.Count(fakeexcel.CountAppendsReceived))
	require.Nil(t, ctx.recordedHeartbeat)
}

func TestAppendTableRowsMatchesNumberAndBooleanKeysByType(t *testing.T) {
	provider := fakeexcel.New(t, excelTestToken, "refresh")
	provider.AddTable(testDriveID, testWorkbookID, fakeexcel.Table{ID: "1", Name: "Invoices", Columns: []string{"InvoiceNumber", "Paid"},
		Rows: [][]any{{1001.0, true}}})
	input := excel.AppendTableRowsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "Invoices", KeyColumn: "InvoiceNumber",
		Rows: []map[string]excel.CellValue{
			{"InvoiceNumber": excel.NumberCellValue(1001), "Paid": excel.BooleanCellValue(true)},
			{"InvoiceNumber": excel.TextCellValue("1001"), "Paid": excel.BooleanCellValue(false)},
		}}
	result, err := sdkgo.RunMutation(newDexContext("typed"), newExcelClient(t, provider.URL).AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, []excel.CellValue{excel.TextCellValue("1001")}, result.Value.AppendedKeys, "text 1001 is not the number 1001")
	require.Equal(t, [][]any{{1001.0, true}, {"1001", false}}, provider.TableRows(testDriveID, testWorkbookID, "1"))
}

func TestAppendTableRowsReportsColumnsTheTableDoesNotHave(t *testing.T) {
	provider := newDecisionsProvider(t)
	client := newExcelClient(t, provider.URL)
	for name, input := range map[string]excel.AppendTableRowsInput{
		"unknown key column": {DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "Decisions", KeyColumn: "Missing",
			Rows: []map[string]excel.CellValue{{"Missing": excel.TextCellValue("x")}}},
		"unknown row column": appendDecisionsInput(map[string]excel.CellValue{"RequestId": excel.TextCellValue("REQ-9"), "Extra": excel.TextCellValue("x")}),
	} {
		result, err := sdkgo.RunMutation(newDexContext("columns"), client.AppendTableRows(), excelConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, excel.AppendTableRowsBranchColumnsMismatch, result.Branch, name)
	}
	require.Zero(t, provider.Count(fakeexcel.CountAppendsReceived))
}

func TestAppendTableRowsRejectsInvalidRowsBeforeAnyRequest(t *testing.T) {
	provider := newDecisionsProvider(t)
	client := newExcelClient(t, provider.URL, excel.Config{MaxCells: 5})
	for name, input := range map[string]excel.AppendTableRowsInput{
		"no rows":        appendDecisionsInput(),
		"missing key":    appendDecisionsInput(map[string]excel.CellValue{"Decision": excel.TextCellValue("approved")}),
		"empty key":      appendDecisionsInput(decisionRow("", "approved", 1)),
		"repeated key":   appendDecisionsInput(decisionRow("REQ-1", "approved", 1), decisionRow("REQ-1", "escalated", 2)),
		"absent value":   appendDecisionsInput(map[string]excel.CellValue{"RequestId": excel.TextCellValue("REQ-1"), "Decision": {}}),
		"blank key name": {DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "Decisions", Rows: []map[string]excel.CellValue{decisionRow("REQ-1", "a", 1)}},
		"too many rows":  appendDecisionsInput(decisionRow("1", "a", 1), decisionRow("2", "a", 1), decisionRow("3", "a", 1), decisionRow("4", "a", 1), decisionRow("5", "a", 1), decisionRow("6", "a", 1)),
	} {
		result, err := sdkgo.RunMutation(newDexContext("invalid"), client.AppendTableRows(), excelConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, excel.AppendTableRowsBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.Count(fakeexcel.CountColumnReads))

	result, err := sdkgo.RunMutation(newDexContext("cells"), client.AppendTableRows(), excelConnection,
		appendDecisionsInput(decisionRow("REQ-1", "a", 1), decisionRow("REQ-2", "a", 1)))
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchDefect, result.Branch, "two rows of three columns exceed five cells")
	require.Zero(t, provider.Count(fakeexcel.CountAppendsReceived))
}
