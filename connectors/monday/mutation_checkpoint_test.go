// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCreateItemCheckpointAllowsOnlyTheNextAttemptAfter503(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errors":[{"message":"SENTINEL"}]}`)
			return
		}
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("stale-retry-marker")
	input := monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"}
	_, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	staleMarker := append([]byte(nil), stepContext.heartbeat...)
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Len(t, provider.requests, 2)
	require.Equal(t, provider.request(0).header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"))
	stepContext.heartbeat = staleMarker
	stepContext.attemptNo = 3
	result, err = sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount(), "a lost claim cannot authorize another POST")
}

func TestAddUpdateRetries503UnderTheSameKey(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"errors":[{"message":"SENTINEL"}]}`)
			return
		}
		writeData(t, response, map[string]any{"create_update": map[string]any{"id": "3300001", "item_id": testItemID}})
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("add-update-503")
	input := monday.AddUpdateInput{ItemID: testItemID, Body: "Update"}
	_, err := sdkgo.RunMutation(stepContext, client.AddUpdate(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, client.AddUpdate(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchAdded, result.Branch)
	require.Equal(t, provider.request(0).header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"))
}

func TestCreateItemWaitsForAnAttemptWithTimeToSend(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("short-deadline")
	deadlineContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stepContext.Context = deadlineContext
	input := monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"}
	_, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
	stepContext.Context = context.Background()
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemRetriesWithoutSendingWhenDispatchCheckpointFails(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	stepContext := newMondayDexContext("failed-dispatch-checkpoint")
	stepContext.recordErr = errors.New("checkpoint unavailable")
	_, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestCreateItemDoesNotRetryAnInterruptedAcceptedResponse(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("Content-Length", "1000")
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte(`{"data":{"create_item":`))
		require.NoError(t, err)
		response.(http.Flusher).Flush()
		dropConnection(t, response)
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("interrupted-accepted"), newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemDoesNotTransportReplayALostResponseOnAReusedConnection(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		if request.header.Get("Idempotency-Key") == "" {
			writeData(t, response, map[string]any{"items": []any{itemJSON(testItemID, "Existing")}})
			return
		}
		dropConnection(t, response)
	})
	client := newMondayClient(t, provider.URL)
	_, err := sdkgo.RunQuery(newMondayDexContext("prime-connection"), client.GetItem(), mondayConnection, monday.GetItemInput{ItemID: testItemID})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newMondayDexContext("lost-response-reused-connection"), client.CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount(), "Go's transport must not replay a sent mutation")
}

func TestCreateItemCutsOffAnInFlightRequestBeforeTheAttemptDeadline(t *testing.T) {
	var requests atomic.Int32
	release := make(chan struct{})
	defer close(release)
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		<-release
	}))
	t.Cleanup(provider.Close)
	stepContext := newMondayDexContext("in-flight-deadline")
	deadlineContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stepContext.Context = deadlineContext
	startedAt := time.Now()
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.EqualValues(t, 1, requests.Load())
	require.Less(t, time.Since(startedAt), 4*time.Second, "HTTP must end before the Step deadline")
}
