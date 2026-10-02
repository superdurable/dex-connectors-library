// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const policyColumnsBody = `{"value":[{"id":"2","index":1,"name":"MaxAutoApproveUsd"},{"id":"1","index":0,"name":"Category"},{"id":"3","index":2,"name":"Approver"}]}`

func policyTableInput() excel.GetTableRowsInput {
	return excel.GetTableRowsInput{DriveID: testDriveID, WorkbookID: testWorkbookID, Table: "{6D182180-0000-4000-8000-000000000001}"}
}

func TestGetTableRowsKeysEveryDataRowByColumnNameWithoutASession(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.HasSuffix(request.path, "/columns") {
			writeJSON(t, response, http.StatusOK, policyColumnsBody)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"address":"Policy!A2:C3","rowCount":2,"columnCount":3,"values":[["travel",500,"lead@example.com"],["hardware",1500.5,""]]}`)
	})

	result, err := sdkgo.RunQuery(newDexContext("read-policy"), newExcelClient(t, server.URL).GetTableRows(), excelConnection, policyTableInput())
	require.NoError(t, err)
	require.Equal(t, excel.GetTableRowsBranchRead, result.Branch)
	require.Equal(t, []string{"Category", "MaxAutoApproveUsd", "Approver"}, result.Value.Columns)
	require.Len(t, result.Value.Rows, 2)
	limit, _ := result.Value.Rows[1].Values["MaxAutoApproveUsd"].Number()
	require.Equal(t, 1500.5, limit)
	approver, _ := result.Value.Rows[1].Value("Approver")
	require.True(t, approver.IsEmpty())
	require.Equal(t, 1, result.Value.Rows[1].Index)
	require.Equal(t, testRequestID, result.Receipt.ProviderRequestID)

	requests := server.recorded()
	require.Len(t, requests, 2)
	tablePath := testWorkbookURL + "/tables/{6D182180-0000-4000-8000-000000000001}"
	require.Equal(t, tablePath+"/columns", requests[0].path)
	require.Equal(t, "$select=id,index,name", requests[0].rawQuery)
	require.Equal(t, tablePath+"/dataBodyRange", requests[1].path)
	require.Equal(t, "$select=address,rowCount,columnCount,values", requests[1].rawQuery)
	for _, request := range requests {
		require.Equal(t, http.MethodGet, request.method)
		require.Equal(t, "Bearer "+excelTestToken, request.header.Get("Authorization"))
		require.Empty(t, request.header.Get("workbook-session-id"), "requests are sessionless")
	}
}

func TestGetTableRowsClassifiesGraphErrorsWithoutProviderText(t *testing.T) {
	for name, scenario := range map[string]struct {
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		"missing table":           {http.StatusNotFound, graphErrorBody("itemNotFound", ""), excel.GetTableRowsBranchNotFound, false},
		"missing permission":      {http.StatusForbidden, graphErrorBody("accessDenied", ""), excel.GetTableRowsBranchProviderRejected, false},
		"unspecified Excel error": {http.StatusInternalServerError, graphErrorBody("internalServerError", "internalServerErrorUncategorized"), excel.GetTableRowsBranchProviderRejected, false},
		"unsupported workbook":    {http.StatusBadRequest, graphErrorBody("badRequest", "unsupportedWorkbook"), excel.GetTableRowsBranchProviderRejected, false},
		"plain server error":      {http.StatusInternalServerError, graphErrorBody("internalServerError", ""), "", true},
		"gateway timeout":         {http.StatusGatewayTimeout, graphErrorBody("gatewayTimeout", "gatewayTimeoutUncategorized"), "", true},
		"workbook locked":         {http.StatusConflict, graphErrorBody("conflict", "accessConflict"), "", true},
		"transient 400":           {http.StatusBadRequest, graphErrorBody("badRequest", "transientFailure"), "", true},
	} {
		t.Run(name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, scenario.status, scenario.body)
			})
			result, err := sdkgo.RunQuery(newDexContext("read-error"), newExcelClient(t, server.URL).GetTableRows(), excelConnection, policyTableInput())
			if scenario.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
			requireSecretFree(t, result)
		})
	}
}

func TestGetTableRowsHonorsRetryAfterWhenThrottled(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		response.Header().Set("Retry-After", "7")
		writeJSON(t, response, http.StatusTooManyRequests, graphErrorBody("tooManyRequests", "tooManyRequestsUncategorized"))
	})
	_, err := sdkgo.RunQuery(newDexContext("throttled"), newExcelClient(t, server.URL).GetTableRows(), excelConnection, policyTableInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	requireRetryAfter(t, err, 7*time.Second)
}
