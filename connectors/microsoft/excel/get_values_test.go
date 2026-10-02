// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func summaryValuesInput(address string) excel.GetValuesInput {
	return excel.GetValuesInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Worksheet: "Daily Summary", Address: address}
}

func TestGetValuesReadsRawValuesAndDisplayedText(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"address":"'Daily Summary'!A2:C2","rowCount":1,"columnCount":3,
			"values":[["REQ-1",46296,true]],"text":[["REQ-1","10/1/2026","TRUE"]],"formulas":[["","",""]]}`)
	})
	result, err := sdkgo.RunQuery(newDexContext("read-range"), newExcelClient(t, server.URL).GetValues(), excelConnection, summaryValuesInput("a2:c2"))
	require.NoError(t, err)
	require.Equal(t, excel.GetValuesBranchRead, result.Branch)
	require.Equal(t, "'Daily Summary'!A2:C2", result.Value.Address)
	require.Equal(t, 1, result.Value.RowCount)
	require.Equal(t, 3, result.Value.ColumnCount)
	serial, _ := result.Value.Values[0][1].Number()
	require.Equal(t, float64(46296), serial)
	isTrue, _ := result.Value.Values[0][2].Boolean()
	require.True(t, isTrue)
	require.Equal(t, []string{"REQ-1", "10/1/2026", "TRUE"}, result.Value.Text[0])

	requests := server.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, testWorkbookURL+"/worksheets/Daily Summary/range(address='A2:C2')", requests[0].path)
	require.Equal(t, "$select=address,rowCount,columnCount,values,text", requests[0].rawQuery)
}

func TestGetValuesEscapesAWorksheetIDInThePath(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"address":"Summary!B3","rowCount":1,"columnCount":1,"values":[[""]],"text":[[""]]}`)
	})
	input := summaryValuesInput("B3")
	input.Worksheet = "{00000000-0001-0000-0100-000000000000}"
	result, err := sdkgo.RunQuery(newDexContext("by-id"), newExcelClient(t, server.URL).GetValues(), excelConnection, input)
	require.NoError(t, err)
	require.True(t, result.Value.Values[0][0].IsEmpty())
	require.Equal(t, testWorkbookURL+"/worksheets/{00000000-0001-0000-0100-000000000000}/range(address='B3')", server.recorded()[0].path)
}

func TestGetValuesRejectsAResponseOfAnotherSize(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"address":"Summary!A1:B1","rowCount":1,"columnCount":2,"values":[[1,2]],"text":[["1","2"]]}`)
	})
	result, err := sdkgo.RunQuery(newDexContext("size"), newExcelClient(t, server.URL).GetValues(), excelConnection, summaryValuesInput("A1:C1"))
	require.NoError(t, err)
	require.Equal(t, excel.GetValuesBranchInvalidResponse, result.Branch)
}

func TestGetValuesRejectsAddressesOutsideTheContractBeforeAnyRequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	client := newExcelClient(t, server.URL, excel.Config{MaxCells: 100})
	for _, address := range []string{
		"", "A:A", "1:1", "Summary!A1", "$A$1", "A0", "XFE1", "A1048577", "C3:A1", "A1:B2:C3", "A1:J11",
	} {
		result, err := sdkgo.RunQuery(newDexContext("address"), client.GetValues(), excelConnection, summaryValuesInput(address))
		require.NoError(t, err, address)
		require.Equal(t, excel.GetValuesBranchDefect, result.Branch, address)
	}
	for _, worksheet := range []string{"", "a/b", "[x]", "'quoted", "ThisWorksheetNameIsLongerThan31Chars"} {
		input := summaryValuesInput("A1")
		input.Worksheet = worksheet
		result, err := sdkgo.RunQuery(newDexContext("worksheet"), client.GetValues(), excelConnection, input)
		require.NoError(t, err, worksheet)
		require.Equal(t, excel.GetValuesBranchDefect, result.Branch, worksheet)
	}
}
