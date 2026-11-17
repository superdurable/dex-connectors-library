// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/go"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel/go/internal/fakeexcel"
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
	requireMarkerState(t, first.recordedHeartbeat, "claimed")

	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
	require.True(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, []excel.CellValue{excel.TextCellValue("REQ-7")}, result.Value.AlreadyPresentKeys)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
	require.Len(t, provider.TableRows(testDriveID, testWorkbookID, "Decisions"), 2, "the empty first row and one appended row")
}

func TestAppendTableRowsResendsAnInvisibleAppendUntilTheSendCapThenSelectsUncertain(t *testing.T) {
	provider := newDecisionsProvider(t)
	hidden := fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true}
	provider.QueueAppendBehaviors(hidden, hidden, hidden)
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-8", "approved", 80))

	attempt := newDexContext("hidden")
	for send := 1; send <= 3; send++ {
		_, err := sdkgo.RunMutation(attempt, client.AppendTableRows(), excelConnection, input)
		requireRetry(t, err, sdkgo.FailureAvailability)
		require.Equal(t, send, provider.Count(fakeexcel.CountAppendsReceived), "each attempt below the cap resends once")
		attempt = attempt.nextAttempt()
	}
	result, err := sdkgo.RunMutation(attempt, client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchUncertain, result.Branch)
	require.Contains(t, result.Failure.Message, "send cap")
	require.Equal(t, 3, provider.Count(fakeexcel.CountAppendsReceived), "the attempt at the cap only reads the key column")
	requireSecretFree(t, result)
}

func TestAppendTableRowsWithSendCapOneReadsBackOnceAndSelectsUncertain(t *testing.T) {
	provider := newDecisionsProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true})
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-11", "approved", 110))

	first := newDexContext("cap-one")
	_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input, sdkgo.WithMaxSends(1))
	requireRetry(t, err, sdkgo.FailureAvailability)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
}

func TestAppendTableRowsRetriesARefusedAppendAndRecordsThatItMayResend(t *testing.T) {
	for name, status := range map[string]int{"throttled": http.StatusTooManyRequests, "unavailable": http.StatusServiceUnavailable} {
		t.Run(name, func(t *testing.T) {
			provider := newDecisionsProvider(t)
			provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusWithoutApplying: status, RetryAfterSeconds: 4})
			client := newExcelClient(t, provider.URL)
			input := appendDecisionsInput(decisionRow("REQ-9", "approved", 90))

			first := newDexContext("refused")
			_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
			requireRetryAfter(t, err, 4*time.Second)
			requireMarkerState(t, first.recordedHeartbeat, "canResend")

			result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
			require.NoError(t, err)
			require.Equal(t, excel.AppendTableRowsBranchAppended, result.Branch)
			require.Equal(t, []excel.CellValue{excel.TextCellValue("REQ-9")}, result.Value.AppendedKeys)
			require.Equal(t, 2, provider.Count(fakeexcel.CountAppendsReceived))
			require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsApplied))
		})
	}
}

func TestAppendTableRowsMapsARejectedFirstAppendAndNeverResendsIt(t *testing.T) {
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
	requireMarkerState(t, ctx.recordedHeartbeat, "canResend")
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

func TestAppendTableRowsSelectsUncertainForARefusalAfterAnUnknownOutcome(t *testing.T) {
	provider := newDecisionsProvider(t)
	provider.QueueAppendBehaviors(
		fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true},
		fakeexcel.AppendBehavior{StatusWithoutApplying: http.StatusForbidden},
	)
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-12", "approved", 120))

	first := newDexContext("refused-after-unknown")
	_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	result, err := sdkgo.RunMutation(first.nextAttempt(), client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.Count(fakeexcel.CountAppendsReceived))
}

func TestAppendTableRowsRetriesWithoutAnyRequestWhenTheCheckpointIsUnreadable(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	ctx := newDexContext("unreadable")
	ctx.attempt = 2
	ctx.previousHeartbeat = json.RawMessage(`{"callId":`)
	_, err := sdkgo.RunMutation(ctx, newExcelClient(t, server.URL).AppendTableRows(), excelConnection, appendDecisionsInput(decisionRow("REQ-13", "approved", 130)))
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Empty(t, server.recorded())
}

func TestAppendTableRowsReadsBackButNeverResendsAfterAnOlderReleaseMarker(t *testing.T) {
	provider := newDecisionsProvider(t)
	provider.QueueAppendBehaviors(fakeexcel.AppendBehavior{StatusAfterApplying: http.StatusBadGateway, IsHiddenFromKeyReads: true})
	client := newExcelClient(t, provider.URL)
	input := appendDecisionsInput(decisionRow("REQ-14", "approved", 140))

	first := newDexContext("legacy")
	_, err := sdkgo.RunMutation(first, client.AppendTableRows(), excelConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	var marker struct {
		CallID string `json:"callId"`
	}
	require.NoError(t, json.Unmarshal(first.recordedHeartbeat, &marker))
	legacy := first.nextAttempt()
	legacy.previousHeartbeat = json.RawMessage(`{"microsoftExcelDispatchedCallId":"` + marker.CallID + `"}`)
	result, err := sdkgo.RunMutation(legacy, client.AppendTableRows(), excelConnection, input)
	require.NoError(t, err)
	require.Equal(t, excel.AppendTableRowsBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.Count(fakeexcel.CountAppendsReceived))
}

func requireMarkerState(t *testing.T, heartbeat json.RawMessage, state string) {
	t.Helper()
	var marker struct {
		State string `json:"state"`
	}
	require.NoError(t, json.Unmarshal(heartbeat, &marker))
	require.Equal(t, state, marker.State)
}
