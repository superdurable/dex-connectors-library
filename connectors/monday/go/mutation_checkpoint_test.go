// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type mutationRoundTripFunc func(*http.Request) (*http.Response, error)

func (function mutationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type recordedMutationCheckpoint struct {
	Attempt   int32 `json:"attempt"`
	CanResend bool  `json:"canResend"`
}

func mutationCheckpointForTest(t *testing.T, stepContext *mondayDexContext) recordedMutationCheckpoint {
	t.Helper()
	stepContext.mutex.Lock()
	defer stepContext.mutex.Unlock()
	var checkpoint recordedMutationCheckpoint
	require.NoError(t, json.Unmarshal(stepContext.heartbeat, &checkpoint))
	return checkpoint
}

func TestCheckpointedMutationsRetryDocumentedRefusalsInHTTP200(t *testing.T) {
	for _, operation := range []string{"createItem", "addUpdate"} {
		for _, test := range []struct {
			name string
			body string
			kind sdkgo.FailureKind
		}{
			{"temporarily blocked", `{"errors":[{"extensions":{"code":"API_TEMPORARILY_BLOCKED"}}]}`, sdkgo.FailureAvailability},
			{"complexity limit", `{"errors":[{"extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED"}}]}`, sdkgo.FailureRateLimit},
			{"idempotency conflict", `{"errors":[{"extensions":{"code":"IDEMPOTENCY_CONFLICT"}}]}`, sdkgo.FailureConflict},
			{"status code 503", `{"errors":[{"extensions":{"status_code":503}}]}`, sdkgo.FailureAvailability},
		} {
			t.Run(operation+"/"+test.name, func(t *testing.T) {
				provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
					if index == 0 {
						writeJSON(t, response, http.StatusOK, test.body)
						return
					}
					if operation == "createItem" {
						writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
					} else {
						writeData(t, response, map[string]any{"create_update": map[string]any{"id": "3300001", "item_id": testItemID}})
					}
				})
				client := newMondayClient(t, provider.URL)
				stepContext := newMondayDexContext(operation + test.name)
				if operation == "createItem" {
					_, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, test.kind, retry.Failure.Kind)
				} else {
					_, err := sdkgo.RunMutation(stepContext, client.AddUpdate(), mondayConnection, monday.AddUpdateInput{ItemID: testItemID, Body: "Update"})
					var retry *sdkgo.RetryError
					require.ErrorAs(t, err, &retry)
					require.Equal(t, test.kind, retry.Failure.Kind)
				}
				require.Equal(t, recordedMutationCheckpoint{Attempt: 1, CanResend: true}, mutationCheckpointForTest(t, stepContext))
				stepContext.attemptNo = 2
				if operation == "createItem" {
					result, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
					require.NoError(t, err)
					require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
				} else {
					result, err := sdkgo.RunMutation(stepContext, client.AddUpdate(), mondayConnection, monday.AddUpdateInput{ItemID: testItemID, Body: "Update"})
					require.NoError(t, err)
					require.Equal(t, monday.AddUpdateBranchAdded, result.Branch)
				}
				require.Equal(t, 2, provider.requestCount())
				require.Equal(t, provider.request(0).header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"))
			})
		}
	}
}

func TestCreateItemDoesNotPermitResendWhenARefusalAccompaniesCreatedData(t *testing.T) {
	stepContext := newMondayDexContext("data-beside-refusal")
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		body, err := json.Marshal(map[string]any{"data": map[string]any{"create_item": itemJSON("5550001", "Fire drill")},
			"errors": []any{map[string]any{"extensions": map[string]any{"code": "FIELD_LIMIT_EXCEEDED"}}}})
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(body))
	})
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1}, mutationCheckpointForTest(t, stepContext))
	stepContext.attemptNo = 2
	result, err = sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemTreatsHTTP200WithStatusCode500AsUncertain(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"errors":[{"extensions":{"status_code":500}}]}`)
	})
	stepContext := newMondayDexContext("status-code-500")
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1}, mutationCheckpointForTest(t, stepContext))
	require.Equal(t, 1, provider.requestCount())
}

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
	deadlineContext, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	stepContext.Context = deadlineContext
	input := monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"}
	_, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1, CanResend: true}, mutationCheckpointForTest(t, stepContext))
	stepContext.Context = context.Background()
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemRetriesAfterARefusedConnection(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	stepContext := newMondayDexContext("refused-connection")
	input := monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"}
	_, err := sdkgo.RunMutation(stepContext, newMondayClient(t, closedLoopbackURL(t)).CreateItem(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1, CanResend: true}, mutationCheckpointForTest(t, stepContext))
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestAddUpdateDoesNotRetryALostResponse(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	stepContext := newMondayDexContext("lost-update-response")
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).AddUpdate(), mondayConnection,
		monday.AddUpdateInput{ItemID: testItemID, Body: "Update"})
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch)
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1}, mutationCheckpointForTest(t, stepContext))
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemInFlightCheckpointBlocksTheNextAttempt(t *testing.T) {
	stepContext := newMondayDexContext("in-flight-marker")
	var inFlight recordedMutationCheckpoint
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		inFlight = mutationCheckpointForTest(t, stepContext)
		writeJSON(t, response, http.StatusServiceUnavailable, `{"errors":[{}]}`)
	})
	client := newMondayClient(t, provider.URL)
	input := monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"}
	_, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1}, inFlight)
	stepContext.mutex.Lock()
	checkpoint, err := json.Marshal(inFlight)
	require.NoError(t, err)
	stepContext.heartbeat = checkpoint
	stepContext.mutex.Unlock()
	stepContext.attemptNo = 2
	result, err := sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, input)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemTreatsAnUnreadablePriorCheckpointAsUncertain(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	for _, test := range []struct {
		name       string
		checkpoint []byte
		readErr    error
	}{
		{"read failure", nil, errors.New("checkpoint storage unavailable")},
		{"wrong call", []byte(`{"callId":"another-call","attempt":1,"canResend":true}`), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			stepContext := newMondayDexContext("unreadable-checkpoint")
			stepContext.attemptNo = 2
			stepContext.heartbeat = test.checkpoint
			stepContext.readErr = test.readErr
			result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
				monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
			require.NoError(t, err)
			require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
			require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
		})
	}
	require.Zero(t, provider.requestCount())
}

func TestCreateItemDoesNotResendAfterRefreshConsumesTheRequestWindow(t *testing.T) {
	var providerRequests atomic.Int32
	transport := mutationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "auth.monday.com" {
			time.Sleep(2 * time.Second)
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
				`{"access_token":"fresh-token","refresh_token":"rotated","token_type":"Bearer","scope":"boards:read boards:write updates:write"}`))}, nil
		}
		providerRequests.Add(1)
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(
			`{"errors":[{"extensions":{"code":"NOT_AUTHENTICATED"}}]}`))}, nil
	})
	future := time.Now().Add(time.Hour)
	source := testsupport.NewRefreshingCredentialSource(monday.Credentials{
		AuthMethodID: monday.OAuthAuthMethodID, OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString("stale-token"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
	}, &future)
	client, err := monday.New(monday.Config{}, source, monday.WithAPIURL("https://example.invalid/v2"), monday.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	stepContext := newMondayDexContext("refresh-budget")
	deadlineContext, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	stepContext.Context = deadlineContext
	_, err = sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.EqualValues(t, 1, providerRequests.Load(), "the refreshed token must not start a second POST")
	require.Equal(t, recordedMutationCheckpoint{Attempt: 1, CanResend: true}, mutationCheckpointForTest(t, stepContext))
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
	deadlineContext, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	stepContext.Context = deadlineContext
	startedAt := time.Now()
	result, err := sdkgo.RunMutation(stepContext, newMondayClient(t, provider.URL).CreateItem(), mondayConnection,
		monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.EqualValues(t, 1, requests.Load())
	require.Less(t, time.Since(startedAt), 5*time.Second, "HTTP must end before the Step deadline")
}
