// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"math"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func summaryUpdateInput(values ...excel.CellValue) excel.UpdateValuesInput {
	return excel.UpdateValuesInput{
		DriveID: testDriveID, WorkbookID: testWorkbookID, Worksheet: "Summary", Address: "A2:E2",
		Values: [][]excel.CellValue{values},
	}
}

func TestUpdateValuesWritesLiteralTextNumbersBooleansAndBlanks(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"address":"Summary!A2:E2","rowCount":1,"columnCount":5}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("write"), newExcelClient(t, server.URL).UpdateValues(), excelConnection, summaryUpdateInput(
		excel.TextCellValue("=HYPERLINK(\"http://example.com\")"), excel.TextCellValue("00123"),
		excel.NumberCellValue(1250.5), excel.BooleanCellValue(false), excel.EmptyCellValue(),
	))
	require.NoError(t, err)
	require.Equal(t, excel.UpdateValuesBranchUpdated, result.Branch)
	require.Equal(t, excel.UpdateValuesOutput{Address: "Summary!A2:E2", RowCount: 1, ColumnCount: 5}, result.Value)

	request := server.recorded()[0]
	require.Equal(t, http.MethodPatch, request.method)
	require.Equal(t, testWorkbookURL+"/worksheets/Summary/range(address='A2:E2')", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.JSONEq(t, `{"values":[["'=HYPERLINK(\"http://example.com\")","'00123",1250.5,false,""]]}`, string(request.body),
		"text carries Excel's apostrophe prefix so it is never a formula or a number")
}

func TestUpdateValuesRetriesAmbiguousFailuresBecauseTheValuesAreAbsolute(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		response.Header().Set("Retry-After", "3")
		writeJSON(t, response, http.StatusServiceUnavailable, graphErrorBody("serviceNotAvailable", "serviceUnavailableUncategorized"))
	})
	_, err := sdkgo.RunMutation(newDexContext("unavailable"), newExcelClient(t, server.URL).UpdateValues(), excelConnection, summaryUpdateInput(
		excel.TextCellValue("a"), excel.TextCellValue("b"), excel.TextCellValue("c"), excel.TextCellValue("d"), excel.TextCellValue("e"),
	))
	requireRetry(t, err, sdkgo.FailureAvailability)

	closed := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		connection, _, hijackErr := response.(http.Hijacker).Hijack()
		require.NoError(t, hijackErr)
		require.NoError(t, connection.Close())
	})
	_, err = sdkgo.RunMutation(newDexContext("closed"), newExcelClient(t, closed.URL).UpdateValues(), excelConnection, summaryUpdateInput(
		excel.TextCellValue("a"), excel.TextCellValue("b"), excel.TextCellValue("c"), excel.TextCellValue("d"), excel.TextCellValue("e"),
	))
	requireRetry(t, err, sdkgo.FailureTransport)
}

func TestUpdateValuesMapsRejectionsWithoutProviderText(t *testing.T) {
	for name, scenario := range map[string]struct {
		status int
		body   string
		branch sdkgo.BranchID
	}{
		"missing worksheet":  {http.StatusNotFound, graphErrorBody("itemNotFound", ""), excel.UpdateValuesBranchNotFound},
		"protected cells":    {http.StatusForbidden, graphErrorBody("accessDenied", "accessDenied"), excel.UpdateValuesBranchProviderRejected},
		"range too large":    {http.StatusRequestEntityTooLarge, graphErrorBody("payloadTooLarge", ""), excel.UpdateValuesBranchProviderRejected},
		"unspecified failed": {http.StatusInternalServerError, graphErrorBody("internalServerError", "internalServerErrorUncategorized"), excel.UpdateValuesBranchProviderRejected},
	} {
		t.Run(name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, scenario.status, scenario.body)
			})
			result, err := sdkgo.RunMutation(newDexContext("rejected"), newExcelClient(t, server.URL).UpdateValues(), excelConnection, summaryUpdateInput(
				excel.TextCellValue("a"), excel.TextCellValue("b"), excel.TextCellValue("c"), excel.TextCellValue("d"), excel.TextCellValue("e"),
			))
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			requireSecretFree(t, result)
		})
	}
}

func TestUpdateValuesRejectsValuesThatDoNotFillTheRangeBeforeAnyRequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	client := newExcelClient(t, server.URL)
	text := excel.TextCellValue("x")
	for name, input := range map[string]excel.UpdateValuesInput{
		"too few cells":  summaryUpdateInput(text, text, text, text),
		"absent value":   summaryUpdateInput(text, text, excel.CellValue{}, text, text),
		"NaN":            summaryUpdateInput(text, text, excel.NumberCellValue(math.NaN()), text, text),
		"infinite":       summaryUpdateInput(text, text, excel.NumberCellValue(math.Inf(1)), text, text),
		"too many rows":  {DriveID: testDriveID, WorkbookID: testWorkbookID, Worksheet: "Summary", Address: "A1", Values: [][]excel.CellValue{{text}, {text}}},
		"unbounded":      {DriveID: testDriveID, WorkbookID: testWorkbookID, Worksheet: "Summary", Address: "A:A", Values: [][]excel.CellValue{{text}}},
		"blank workbook": {DriveID: testDriveID, Worksheet: "Summary", Address: "A1", Values: [][]excel.CellValue{{text}}},
	} {
		result, err := sdkgo.RunMutation(newDexContext("invalid"), client.UpdateValues(), excelConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, excel.UpdateValuesBranchDefect, result.Branch, name)
	}
}
