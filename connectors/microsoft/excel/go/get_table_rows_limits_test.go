// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func newTableServer(t *testing.T, dataBody string) *recordingServer {
	t.Helper()
	return newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.HasSuffix(request.path, "/columns") {
			writeJSON(t, response, http.StatusOK, policyColumnsBody)
			return
		}
		writeJSON(t, response, http.StatusOK, dataBody)
	})
}

func TestGetTableRowsSelectsTooLargeAboveTheCellLimit(t *testing.T) {
	server := newTableServer(t, `{"rowCount":2,"columnCount":3,"values":[["a",1,"x"],["b",2,"y"]]}`)
	result, err := sdkgo.RunQuery(newDexContext("cells"), newExcelClient(t, server.URL, excel.Config{MaxCells: 5}).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchTooLarge, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestGetTableRowsSelectsTooLargeWhenExcelOmitsValuesOrTheResponseIsOversized(t *testing.T) {
	omitted := newTableServer(t, `{"rowCount":2,"columnCount":3,"values":null}`)
	result, err := sdkgo.RunQuery(newDexContext("omitted"), newExcelClient(t, omitted.URL).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchTooLarge, result.Branch)

	oversized := newTableServer(t, `{"rowCount":1,"columnCount":3,"values":[["`+strings.Repeat("x", 2048)+`",1,"y"]]}`)
	result, err = sdkgo.RunQuery(newDexContext("oversized"), newExcelClient(t, oversized.URL, excel.Config{MaxResponseBytes: 1024}).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchTooLarge, result.Branch)
}

func TestGetTableRowsRejectsAColumnChangeBetweenRequests(t *testing.T) {
	server := newTableServer(t, `{"rowCount":1,"columnCount":4,"values":[["a",1,"x","new"]]}`)
	result, err := sdkgo.RunQuery(newDexContext("changed"), newExcelClient(t, server.URL).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchInvalidResponse, result.Branch)
	require.Contains(t, result.Failure.Message, "read it again")
}

func TestGetTableRowsRejectsInvalidColumnsAndRanges(t *testing.T) {
	for name, columnsBody := range map[string]string{
		"duplicate names":   `{"value":[{"id":"1","index":0,"name":"Key"},{"id":"2","index":1,"name":"Key"}]}`,
		"gap in positions":  `{"value":[{"id":"1","index":0,"name":"Key"},{"id":"2","index":2,"name":"Other"}]}`,
		"missing column ID": `{"value":[{"index":0,"name":"Key"}]}`,
		"no columns":        `{"value":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, http.StatusOK, columnsBody)
			})
			result, err := sdkgo.RunQuery(newDexContext("columns"), newExcelClient(t, server.URL).GetTableRows(), excelConnection, policyTableInput())
			require.NoError(t, err)
			require.Equal(t, excel.GetTableRowsBranchInvalidResponse, result.Branch)
			require.Len(t, server.recorded(), 1, "no data read follows invalid columns")
		})
	}
	ragged := newTableServer(t, `{"rowCount":2,"columnCount":3,"values":[["a",1,"x"],["b",2]]}`)
	result, err := sdkgo.RunQuery(newDexContext("ragged"), newExcelClient(t, ragged.URL).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchInvalidResponse, result.Branch)
}

func TestGetTableRowsRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	client := newExcelClient(t, server.URL)
	for name, input := range map[string]excel.GetTableRowsInput{
		"blank drive":       {WorkbookID: testWorkbookID, Table: "Policy"},
		"drive with slash":  {DriveID: "b!a/b", WorkbookID: testWorkbookID, Table: "Policy"},
		"blank workbook":    {DriveID: testDriveID, Table: "Policy"},
		"blank table":       {DriveID: testDriveID, WorkbookID: testWorkbookID},
		"table with slash":  {DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "a/b"},
		"control character": {DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "Policy\n"},
	} {
		result, err := sdkgo.RunQuery(newDexContext("invalid"), client.GetTableRows(), excelConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, excel.GetTableRowsBranchDefect, result.Branch, name)
	}
}
