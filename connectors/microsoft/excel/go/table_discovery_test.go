// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListTableColumnsReturnsOrderedColumns(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, policyColumnsBody)
	})
	result, err := sdkgo.RunQuery(newDexContext("list-columns"), newExcelClient(t, server.URL).ListTableColumns(), excelConnection,
		excel.ListTableColumnsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "ApprovalPolicy"})
	require.NoError(t, err)
	require.Equal(t, excel.ListTableColumnsBranchListed, result.Branch)
	require.Equal(t, []string{"Category", "MaxAutoApproveUsd", "Approver"}, []string{result.Value.Columns[0].Name, result.Value.Columns[1].Name, result.Value.Columns[2].Name})
	require.Len(t, server.recorded(), 1)
}

func TestFindTableRowMatchesTypedValue(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.HasSuffix(request.path, "/columns") {
			writeJSON(t, response, http.StatusOK, policyColumnsBody)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"address":"Policy!A2:C3","rowCount":2,"columnCount":3,"values":[["travel",500,"lead@example.com"],["hardware",1500.5,""]]}`)
	})
	result, err := sdkgo.RunQuery(newDexContext("find-row"), newExcelClient(t, server.URL).FindTableRow(), excelConnection,
		excel.FindTableRowInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "ApprovalPolicy", Column: "Category", Value: excel.TextCellValue("hardware")})
	require.NoError(t, err)
	require.Equal(t, excel.FindTableRowBranchFound, result.Branch)
	require.Equal(t, 1, result.Value.Row.Index)
	require.Len(t, server.recorded(), 2)
}

func TestListWorksheetsReturnsWorkbookSheets(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"value":[{"id":"{FC034FA8-F8CC-4D24-9C0A-02A81B7792A0}","name":"Policy","position":0,"visibility":"Visible"}]}`)
	})
	result, err := sdkgo.RunQuery(newDexContext("list-sheets"), newExcelClient(t, server.URL).ListWorksheets(), excelConnection,
		excel.ListWorksheetsInput{DriveID: testDriveID, WorkbookID: testWorkbookID})
	require.NoError(t, err)
	require.Equal(t, excel.ListWorksheetsBranchListed, result.Branch)
	require.Equal(t, "Policy", result.Value.Worksheets[0].Name)
	require.Equal(t, testWorkbookURL+"/worksheets", server.recorded()[0].path)
}

func TestTableDiscoveryRetriesUnknownProviderErrors(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
			writeJSON(t, response, status, `{}`)
		})
		_, err := sdkgo.RunQuery(newDexContext("retry-columns"), newExcelClient(t, server.URL).ListTableColumns(), excelConnection,
			excel.ListTableColumnsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "ApprovalPolicy"})
		failureKind := sdkgo.FailureAvailability
		if status == http.StatusTooManyRequests {
			failureKind = sdkgo.FailureRateLimit
		}
		requireRetry(t, err, failureKind)
	}
}

func TestDiscoveryQueriesClassifyProviderBranches(t *testing.T) {
	type runQuery func(*excel.Client) (sdkgo.BranchID, error)
	queries := map[string]struct {
		run      runQuery
		branches map[int]sdkgo.BranchID
	}{
		"columns": {
			run: func(client *excel.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newDexContext("columns-branch"), client.ListTableColumns(), excelConnection, excel.ListTableColumnsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "ApprovalPolicy"})
				return result.Branch, err
			},
			branches: map[int]sdkgo.BranchID{404: excel.ListTableColumnsBranchNotFound, 403: excel.ListTableColumnsBranchProviderRejected, 413: excel.ListTableColumnsBranchTooLarge, 200: excel.ListTableColumnsBranchInvalidResponse},
		},
		"row": {
			run: func(client *excel.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newDexContext("row-branch"), client.FindTableRow(), excelConnection, excel.FindTableRowInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "ApprovalPolicy", Column: "Category", Value: excel.TextCellValue("travel")})
				return result.Branch, err
			},
			branches: map[int]sdkgo.BranchID{404: excel.FindTableRowBranchNotFound, 403: excel.FindTableRowBranchProviderRejected, 413: excel.FindTableRowBranchTooLarge, 200: excel.FindTableRowBranchInvalidResponse},
		},
		"worksheets": {
			run: func(client *excel.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newDexContext("worksheets-branch"), client.ListWorksheets(), excelConnection, excel.ListWorksheetsInput{DriveID: testDriveID, WorkbookID: testWorkbookID})
				return result.Branch, err
			},
			branches: map[int]sdkgo.BranchID{404: excel.ListWorksheetsBranchNotFound, 403: excel.ListWorksheetsBranchProviderRejected, 413: excel.ListWorksheetsBranchTooLarge, 200: excel.ListWorksheetsBranchInvalidResponse},
		},
		"workbooks": {
			run: func(client *excel.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newDexContext("workbooks-branch"), client.ListWorkbooks(), excelConnection, excel.ListWorkbooksInput{DriveID: testDriveID})
				return result.Branch, err
			},
			branches: map[int]sdkgo.BranchID{404: excel.ListWorkbooksBranchNotFound, 403: excel.ListWorkbooksBranchProviderRejected, 413: excel.ListWorkbooksBranchTooLarge, 200: excel.ListWorkbooksBranchInvalidResponse},
		},
	}
	for name, query := range queries {
		for status, expected := range query.branches {
			t.Run(name+"/"+http.StatusText(status), func(t *testing.T) {
				server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
					writeJSON(t, response, status, `{}`)
				})
				branch, err := query.run(newExcelClient(t, server.URL))
				require.NoError(t, err)
				require.Equal(t, expected, branch)
			})
		}
	}
}

func TestCreateWorksheetUsesNamedReadBackAndOnePost(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusNotFound, `{}`)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"id":"{FC034FA8-F8CC-4D24-9C0A-02A81B7792A2}","name":"Audit","position":2,"visibility":"Visible"}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("create-sheet"), newExcelClient(t, server.URL).CreateWorksheet(), excelConnection,
		excel.CreateWorksheetInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Name: "Audit"})
	require.NoError(t, err)
	require.Equal(t, excel.CreateWorksheetBranchCreated, result.Branch)
	require.Equal(t, "Audit", result.Value.Worksheet.Name)
	requests := server.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodPost, requests[1].method)
	require.JSONEq(t, `{"name":"Audit"}`, string(requests[1].body))
}
