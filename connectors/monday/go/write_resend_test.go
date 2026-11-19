// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

type mutationRoundTripFunc func(*http.Request) (*http.Response, error)

func (function mutationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// dispatchMarkerForTest reads the SDK fence marker the last attempt stored.
func dispatchMarkerForTest(t *testing.T, stepContext *mondayDexContext) singledispatch.Marker {
	t.Helper()
	stepContext.mutex.Lock()
	defer stepContext.mutex.Unlock()
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(stepContext.heartbeat, &marker))
	return marker
}

func createItemForTest(t *testing.T, stepContext *mondayDexContext, client *monday.Client) (sdkgo.MutationResult[monday.CreateItemOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(stepContext, client.CreateItem(), mondayConnection, monday.CreateItemInput{BoardID: testBoardID, ItemName: "Fire drill"})
}

func addUpdateForTest(t *testing.T, stepContext *mondayDexContext, client *monday.Client) (sdkgo.MutationResult[monday.AddUpdateOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(stepContext, client.AddUpdate(), mondayConnection, monday.AddUpdateInput{ItemID: testItemID, Body: "Update"})
}

func TestFencedWritesRetryProvenUnprocessedRefusalsWithoutCountingASend(t *testing.T) {
	for _, operation := range []string{"createItem", "addUpdate"} {
		for _, test := range []struct {
			name string
			body string
			kind sdkgo.FailureKind
		}{
			{"temporarily blocked", `{"errors":[{"extensions":{"code":"API_TEMPORARILY_BLOCKED"}}]}`, sdkgo.FailureAvailability},
			{"complexity limit", `{"errors":[{"extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED"}}]}`, sdkgo.FailureRateLimit},
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
				var err error
				if operation == "createItem" {
					_, err = createItemForTest(t, stepContext, client)
				} else {
					_, err = addUpdateForTest(t, stepContext, client)
				}
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.kind, retry.Failure.Kind)
				marker := dispatchMarkerForTest(t, stepContext)
				require.Equal(t, singledispatch.StateCanResend, marker.State)
				require.Zero(t, marker.SendCount, "an unprocessed send is not counted against the cap")
				stepContext.attemptNo = 2
				if operation == "createItem" {
					result, err := createItemForTest(t, stepContext, client)
					require.NoError(t, err)
					require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
				} else {
					result, err := addUpdateForTest(t, stepContext, client)
					require.NoError(t, err)
					require.Equal(t, monday.AddUpdateBranchAdded, result.Branch)
				}
				require.Equal(t, 2, provider.requestCount())
				require.Equal(t, provider.request(0).header.Get("Idempotency-Key"), provider.request(1).header.Get("Idempotency-Key"))
				accepted := dispatchMarkerForTest(t, stepContext)
				require.Equal(t, singledispatch.StateClaimed, accepted.State)
				require.NotEmpty(t, accepted.ProviderID)
				require.EqualValues(t, 1, accepted.SendCount)
			})
		}
	}
}

func TestFencedWritesResendUnknownOutcomesUpToTheSendCap(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(*testing.T, http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{"server error", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL"}]}`)
		}, sdkgo.FailureAvailability},
		{"HTTP 200 with status code 500", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"errors":[{"extensions":{"status_code":500}}]}`)
		}, sdkgo.FailureAvailability},
		{"idempotency conflict", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusConflict, `{"errors":[{"extensions":{"code":"IDEMPOTENCY_CONFLICT"}}]}`)
		}, sdkgo.FailureConflict},
		{"lost response", func(t *testing.T, response http.ResponseWriter) { dropConnection(t, response) }, sdkgo.FailureTransport},
		{"unlisted status", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTeapot, `{"errors":[{"message":"SENTINEL"}]}`)
		}, sdkgo.FailureProviderRejection},
		{"item without an ID", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"create_item":null}}`)
		}, sdkgo.FailureProtocol},
		{"reflected token", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"create_item":{"id":"1","name":"`+testAPIToken+`"}}}`)
		}, sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(t, response) })
			client := newMondayClient(t, provider.URL)
			stepContext := newMondayDexContext("unknown-" + test.name)
			for attempt := int32(1); attempt <= 2; attempt++ {
				stepContext.attemptNo = attempt
				_, err := createItemForTest(t, stepContext, client)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "attempt %d stays below the send cap", attempt)
				marker := dispatchMarkerForTest(t, stepContext)
				require.Equal(t, singledispatch.StateClaimed, marker.State)
				require.Equal(t, attempt, marker.SendCount)
			}
			stepContext.attemptNo = 3
			result, err := createItemForTest(t, stepContext, client)
			require.NoError(t, err)
			require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 3, provider.requestCount(), "the default cap allows three sends")
			for index := 1; index < 3; index++ {
				require.Equal(t, provider.request(0).header.Get("Idempotency-Key"), provider.request(index).header.Get("Idempotency-Key"))
			}
			stepContext.attemptNo = 4
			result, err = createItemForTest(t, stepContext, client)
			require.NoError(t, err)
			require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
			require.Equal(t, 3, provider.requestCount(), "nothing is sent at the cap")
		})
	}
}

func TestFencedWritesNeverResendAPossiblyAcceptedWrite(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(*testing.T, http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{"refusal beside an item value", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"create_item":{"id":"SENTINEL"}},"errors":[{"extensions":{"code":"ColumnValueException"}}]}`)
		}, sdkgo.FailureValidation},
		{"item value without an ID", func(t *testing.T, response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"data":{"create_item":{"name":"Item"}}}`)
		}, sdkgo.FailureProtocol},
		{"2xx body broken after its headers", func(t *testing.T, response http.ResponseWriter) {
			response.Header().Set("Content-Length", "1000")
			response.WriteHeader(http.StatusOK)
			_, err := response.Write([]byte(`{"data":{"create_item":`))
			require.NoError(t, err)
			response.(http.Flusher).Flush()
			dropConnection(t, response)
		}, sdkgo.FailureTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(t, response) })
			client := newMondayClient(t, provider.URL)
			stepContext := newMondayDexContext("accepted-" + test.name)
			result, err := createItemForTest(t, stepContext, client)
			require.NoError(t, err)
			require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestFencedWritesKeepCountingAfterARateLimitedResend(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 1 {
			writeJSON(t, response, http.StatusTooManyRequests, `{"errors":[{"extensions":{"code":"RATE_LIMIT_EXCEEDED"}}]}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL"}]}`)
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("rate-limited-resend")
	// Attempt one is unknown, attempt two is rate limited, and attempt three is unknown again.
	for attempt := int32(1); attempt <= 3; attempt++ {
		stepContext.attemptNo = attempt
		_, err := createItemForTest(t, stepContext, client)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d", attempt)
	}
	stepContext.attemptNo = 4
	result, err := createItemForTest(t, stepContext, client)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 4, provider.requestCount())
}

func TestFencedWritesCredentialFailureAfterARateLimitedResendNeverRecordsNotSent(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 1 {
			writeJSON(t, response, http.StatusTooManyRequests, `{"errors":[{"extensions":{"code":"RATE_LIMIT_EXCEEDED"}}]}`)
			return
		}
		writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL"}]}`)
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("credential-after-rate-limit")
	for attempt := int32(1); attempt <= 2; attempt++ {
		stepContext.attemptNo = attempt
		_, err := addUpdateForTest(t, stepContext, client)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d", attempt)
	}
	before := dispatchMarkerForTest(t, stepContext)
	require.Equal(t, singledispatch.StateCanResend, before.State)
	// The canResend marker gives sendAllowed with one possible earlier send.
	broken, err := monday.New(monday.Config{}, failingMondayCredentials{projectconfig.ErrObjectNotFound}, monday.WithAPIURL(provider.URL+"/v2"))
	require.NoError(t, err)
	stepContext.attemptNo = 3
	result, err := addUpdateForTest(t, stepContext, broken)
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch)
	require.Equal(t, before, dispatchMarkerForTest(t, stepContext))
	require.Equal(t, 2, provider.requestCount())
}

func TestFencedWritesTreatAnOversizedRefusalBodyAsUnknown(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusForbidden, `{"errors":[{"message":"`+strings.Repeat("x", 4096)+`"}]}`)
	})
	client, err := monday.New(monday.Config{MaxResponseBytes: 1024}, testCredentialProvider(), monday.WithAPIURL(provider.URL+"/v2"))
	require.NoError(t, err)
	stepContext := newMondayDexContext("oversized-refusal")
	for attempt := int32(1); attempt < 3; attempt++ {
		stepContext.attemptNo = attempt
		_, err := addUpdateForTest(t, stepContext, client)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "an unreadable refusal is unknown, so it resends below the cap")
	}
	stepContext.attemptNo = 3
	result, err := addUpdateForTest(t, stepContext, client)
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch)
	require.Equal(t, 3, provider.requestCount())
}

// failingMondayCredentials fails every credential resolution with err.
type failingMondayCredentials struct{ err error }

func (credentials failingMondayCredentials) Resolve(sdkgo.Call) (monday.Credentials, error) {
	return monday.Credentials{}, credentials.err
}

func (credentials failingMondayCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[monday.Credentials]) (monday.Credentials, error) {
	return credentials.Resolve(call)
}

func TestFencedWritesRetryATransientCredentialFailureAndSelectDefectOnlyForATerminalOne(t *testing.T) {
	unavailable := fmt.Errorf("read shared credential: %w", projectconfig.ErrCredentialBrokerUnavailable)
	for _, test := range []struct {
		name        string
		err         error
		isTransient bool
	}{
		{"broker unavailable", unavailable, true},
		{"store read", errors.New("project storage read timed out"), true},
		{"connection not configured", errors.New("connection is not configured"), true},
		{"credential not refreshed", projectconfig.ErrRejectedCredentialNotRefreshed, true},
		{"project reauthorization", projectconfig.ErrReauthorizationRequired, false},
		{"object not found", projectconfig.ErrObjectNotFound, false},
		{"connection not selected", projectconfig.ErrConnectionNotSelected, false},
		{"broker rejected", projectconfig.ErrCredentialBrokerRejected, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{}]}`)
			})
			broken, err := monday.New(monday.Config{}, failingMondayCredentials{test.err}, monday.WithAPIURL(provider.URL+"/v2"))
			require.NoError(t, err)

			firstSend := newMondayDexContext("credential-first-" + test.name)
			result, err := addUpdateForTest(t, firstSend, broken)
			if test.isTransient {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "a transient credential failure before any send retries")
				require.Equal(t, singledispatch.StateNotSent, dispatchMarkerForTest(t, firstSend).State)
			} else {
				require.NoError(t, err)
				require.Equal(t, monday.AddUpdateBranchDefect, result.Branch)
			}

			afterSend := newMondayDexContext("credential-after-" + test.name)
			_, err = addUpdateForTest(t, afterSend, newMondayClient(t, provider.URL))
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			before := dispatchMarkerForTest(t, afterSend)
			afterSend.attemptNo = 2
			result, err = addUpdateForTest(t, afterSend, broken)
			if test.isTransient {
				require.ErrorAs(t, err, &retry, "a transient credential failure after a possible send retries")
			} else {
				require.NoError(t, err)
				require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch)
			}
			require.Equal(t, before, dispatchMarkerForTest(t, afterSend), "a possible earlier send is never hidden by notSent")
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestFencedWritesHonorTheApplicationSendCap(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	client := newMondayClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newMondayDexContext("cap-one"), client.AddUpdate(), mondayConnection,
		monday.AddUpdateInput{ItemID: testItemID, Body: "Update"}, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch, "a cap of one never resends")
	require.Equal(t, 1, provider.requestCount())
}

func TestFencedWritesSelectDefiniteRefusalsOnlyForAFirstSend(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"permission", http.StatusForbidden, `{"errors":[{"extensions":{"code":"UserUnauthorizedException"}}]}`, monday.AddUpdateBranchProviderRejected},
		{"missing item", http.StatusOK, `{"data":{"create_update":null},"errors":[{"extensions":{"code":"InvalidItemIdException"}}]}`, monday.AddUpdateBranchNotFound},
		{"rejected document", http.StatusBadRequest, `{"errors":[{"message":"SENTINEL"}]}`, monday.AddUpdateBranchDefect},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{}]}`)
					return
				}
				writeJSON(t, response, test.status, test.body)
			})
			client := newMondayClient(t, provider.URL)
			firstSend := newMondayDexContext("first-send-" + test.name)
			result, err := addUpdateForTest(t, firstSend, newMondayClient(t, newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, test.status, test.body)
			}).URL))
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, singledispatch.StateCanResend, dispatchMarkerForTest(t, firstSend).State, "a refusal records that nothing was written")

			resend := newMondayDexContext("resend-" + test.name)
			_, err = addUpdateForTest(t, resend, client)
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			resend.attemptNo = 2
			result, err = addUpdateForTest(t, resend, client)
			require.NoError(t, err)
			require.Equal(t, monday.AddUpdateBranchUncertain, result.Branch, "the earlier unknown send may have written")
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestFencedWritesNeverResendAnIdentifiedWrite(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	client := newMondayClient(t, provider.URL)
	stepContext := newMondayDexContext("identified-write")
	result, err := createItemForTest(t, stepContext, client)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, "5550001", dispatchMarkerForTest(t, stepContext).ProviderID)
	stepContext.attemptNo = 2
	result, err = createItemForTest(t, stepContext, client)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemRetriesAnUnreadablePriorCheckpointWithoutSending(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	for _, test := range []struct {
		name       string
		checkpoint []byte
		readErr    error
	}{
		{"read failure", nil, errors.New("checkpoint storage unavailable")},
		{"wrong call", []byte(`{"callId":"8a2b6f7e-61b9-4d1a-9c55-2f3c1e0b9d41","attempt":1,"state":"canResend"}`), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			stepContext := newMondayDexContext("unreadable-checkpoint")
			stepContext.attemptNo = 2
			stepContext.heartbeat = test.checkpoint
			stepContext.readErr = test.readErr
			_, err := createItemForTest(t, stepContext, newMondayClient(t, provider.URL))
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "an untrusted checkpoint never selects a terminal branch")
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
		})
	}
	require.Zero(t, provider.requestCount())
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
	_, err := createItemForTest(t, stepContext, client)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
	require.Equal(t, singledispatch.StateNotSent, dispatchMarkerForTest(t, stepContext).State)
	stepContext.Context = context.Background()
	stepContext.attemptNo = 2
	result, err := createItemForTest(t, stepContext, client)
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreateItemRetriesAfterARefusedConnection(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Fire drill")})
	})
	stepContext := newMondayDexContext("refused-connection")
	_, err := createItemForTest(t, stepContext, newMondayClient(t, closedLoopbackURL(t)))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Equal(t, singledispatch.StateCanResend, dispatchMarkerForTest(t, stepContext).State)
	stepContext.attemptNo = 2
	result, err := createItemForTest(t, stepContext, newMondayClient(t, provider.URL))
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
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
	_, err = createItemForTest(t, stepContext, client)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.EqualValues(t, 1, providerRequests.Load(), "the refreshed token must not start a second POST")
	require.Equal(t, singledispatch.StateCanResend, dispatchMarkerForTest(t, stepContext).State, "HTTP 401 proves nothing was written")
}

func TestCreateItemRetriesWithoutSendingWhenDispatchCheckpointFails(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	stepContext := newMondayDexContext("failed-dispatch-checkpoint")
	stepContext.recordErr = errors.New("checkpoint unavailable")
	_, err := createItemForTest(t, stepContext, newMondayClient(t, provider.URL))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Zero(t, provider.requestCount())
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
	_, err = createItemForTest(t, newMondayDexContext("lost-response-reused-connection"), client)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the next attempt resends under the same key")
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
	_, err := createItemForTest(t, stepContext, newMondayClient(t, provider.URL))
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.EqualValues(t, 1, requests.Load())
	require.Less(t, time.Since(startedAt), 5*time.Second, "HTTP must end before the Step deadline")
	require.EqualValues(t, 1, dispatchMarkerForTest(t, stepContext).SendCount, "the cut-off send counts against the cap")
}
