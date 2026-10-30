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
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/internal/fakeexcel"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAppendTableRowsAfterAnAmbiguousResponseFindsItsRowsInsteadOfResending(t *testing.T) {
	provider := newDecisionsProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusGatewayTimeout})
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-7", "approved", 70))

	first := newDexContext("ambiguous")
	_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	requireRetryAfter(t, err, 10*time.Second)
	require.NotNil(t, first.recordedHeartbeat, "an unknown outcome keeps the checkpoint")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
	require.True(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, []excel.CellValue{excel.TextCellValue("REQ-7")}, result.Value.AlreadyPresentKeys)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.Len(t, provider.TableRows(testDriveID, testWorkbookID, "Decisions"), 2, "the empty first row and one appended row")
}

func TestAppendTableRowsNeverResendsWhenTheEarlierAppendIsNotVisible(t *testing.T) {
	provider := newDecisionsProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true})
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-8", "approved", 80))

	first := newDexContext("hidden")
	_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchUncertain, result.Branch)
	require.Contains(t, result.Failure.Message, "earlier attempt")
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived), "the uncertain attempt sends nothing")
	requireSecretFree(t, result)
}

func TestAppendTableRowsRetriesARefusedAppendAndClearsTheCheckpoint(t *testing.T) {
	for name, status := range map[string]int{"throttled": http.StatusTooManyRequests, "unavailable": http.StatusServiceUnavailable} {
		t.Run(name, func(t *testing.T) {
			provider := newDecisionsProvider(t)
			provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusWithoutApplying: status, RetryAfterSeconds: 4})
			client := newExcelClient(t, provider.URL)
			input := appendDecisionsInput(decisionRow("REQ-9", "approved", 90))

			first := newDexContext("refused")
			_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
			requireRetryAfter(t, err, 4*time.Second)
			require.Nil(t, first.recordedHeartbeat, "a refused append clears the checkpoint")

			result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
			require.NoError(t, err)
			require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
			require.Equal(t, []excel.CellValue{excel.TextCellValue("REQ-9")}, result.Value.AppendedKeys)
			require.Equal(t, 2, provider.Count(fakeexcel.CountAppendsReceived))
			require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsApplied))
		})
	}
}

func TestAppendTableRowsMapsARejectedAppendAndSendsNothingWithoutACheckpoint(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		switch request.method {
		case http.MethodPost:
			writeJSON(t, response, http.StatusForbidden, graphErrorBody("accessDenied", "accessDenied"))
		case http.MethodGet:
			if strings.HasSuffix(request.path, "/columns") {
				writeJSON(t, response, http.StatusOK, `{"value":[{"id":"1","index":0,"name":"RequestId"}]}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"rowCount":1,"columnCount":1,"values":[[""]]}`)
		}
	})
	client := newExcelClient(t, server.URL)
	input := appendDecisionsInput(map[string]excel.CellValue{"RequestId": excel.TextCellValue("REQ-10")})
	ctx := newDexContext("rejected")
	result, err := sdkgo.RunMutation(ctx, client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Nil(t, ctx.recordedHeartbeat)
	requireSecretFree(t, result)
	requests := server.recorded()
	require.Len(t, requests, 3)
	require.Equal(t, testWorkbookURL+"/tables/"+decisionsTableID+"/columns/1/dataBodyRange", requests[1].path)
	require.Equal(t, testWorkbookURL+"/tables/"+decisionsTableID+"/rows/add", requests[2].path)
	require.JSONEq(t, `{"values":[["'REQ-10"]]}`, string(requests[2].body))

	rejecting := newDexContext("heartbeat-rejected")
	rejecting.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(rejecting, client.AppendTableRows(), excelConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Len(t, server.recorded(), 5, "the reads ran, but no append was sent without a recorded checkpoint")
}
