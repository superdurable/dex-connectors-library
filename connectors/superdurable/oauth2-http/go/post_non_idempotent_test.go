// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http_test

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	oauth2http "github.com/superdurable/dex-connectors-library/connectors/superdurable/oauth2-http/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestPostNonIdempotentSendsOnePOSTWithoutAnIdempotencyKey(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.api = func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(response, `{"id":"order-1"}`)
	}
	context := singledispatchtest.NewContext("oauth2-http-flow", "create-order")

	result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{
		Path: "/orders", Query: map[string][]string{"notify": {"false"}}, Headers: map[string]string{"Accept": "application/json"}, Body: `{"item":"gear"}`,
	})

	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchCompleted, result.Branch)
	require.Equal(t, oauth2http.SendRequestOutput{StatusCode: 201, ContentType: "application/json", Body: `{"id":"order-1"}`}, result.Value)
	requests := session.server.requests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/api/orders", requests[0].path)
	require.Equal(t, "false", requests[0].query.Get("notify"))
	require.Equal(t, `{"item":"gear"}`, requests[0].body)
	require.Equal(t, "application/json", requests[0].header.Get("Content-Type"))
	require.Empty(t, requests[0].header.Get("Idempotency-Key"))
	require.Equal(t, singledispatch.StateClaimed, lastMarker(t, context).State, "the claim is recorded before the POST")
	requireSecretFree(t, result, "access-token-1", "refresh-token-1")
}

func TestPostNonIdempotentSelectsRejectedForADefiniteRefusalOfTheOnlySend(t *testing.T) {
	for _, statusCode := range []int{400, 403, 404, 411, 413, 414, 415, 422} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			session := newTestSession(t, nil, time.Hour)
			session.server.api = func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(statusCode)
				_, _ = io.WriteString(response, "refused")
			}
			context := singledispatchtest.NewContext("oauth2-http-flow", "refused")

			result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})

			require.NoError(t, err)
			require.Equal(t, oauth2http.PostNonIdempotentBranchRejected, result.Branch)
			require.Equal(t, statusCode, result.Value.StatusCode)
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, context).State, "the refusal proves the API did not process the POST")
		})
	}
}

func TestPostNonIdempotentSelectsUncertainForAnUnknownOutcomeAndNeverSendsAgain(t *testing.T) {
	for _, statusCode := range []int{http.StatusFound, http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTeapot, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			session := newTestSession(t, nil, time.Hour)
			session.server.api = func(response http.ResponseWriter, _ *http.Request) {
				response.Header().Set("Location", "http://127.0.0.1:1/elsewhere")
				response.WriteHeader(statusCode)
			}
			context := singledispatchtest.NewContext("oauth2-http-flow", "unknown")

			first, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
			require.NoError(t, err)
			require.Equal(t, oauth2http.PostNonIdempotentBranchUncertain, first.Branch)
			require.Equal(t, statusCode, first.Value.StatusCode)

			context.AdvanceAttempt()
			second, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
			require.NoError(t, err)
			require.Equal(t, oauth2http.PostNonIdempotentBranchUncertain, second.Branch)
			require.Len(t, session.server.requests(), 1, "a possibly processed POST is never sent again")
		})
	}
}

func TestPostNonIdempotentSelectsUncertainWhenTheAnswerIsLostAfterTheSend(t *testing.T) {
	cutOff := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		connection, _, hijackErr := response.(http.Hijacker).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
	}))
	t.Cleanup(cutOff.Close)
	session := newTestSession(t, func(config *oauth2http.Config) { config.APIBaseURL = cutOff.URL }, time.Hour)

	result, err := postNonIdempotent(t, session, singledispatchtest.NewContext("oauth2-http-flow", "lost"), oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
}

func TestPostNonIdempotentRetriesAnUnprocessedPOSTAndSendsItOnTheNextAttempt(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			session := newTestSession(t, nil, time.Hour)
			answers := 0
			session.server.api = func(response http.ResponseWriter, _ *http.Request) {
				answers++
				if answers == 1 {
					response.Header().Set("Retry-After", "4")
					response.WriteHeader(statusCode)
					return
				}
				response.WriteHeader(http.StatusCreated)
			}
			context := singledispatchtest.NewContext("oauth2-http-flow", "throttled")

			_, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
			var retryAfter *dex.RetryAfterError
			require.ErrorAs(t, err, &retryAfter)
			require.Equal(t, 4*time.Second, retryAfter.After)
			require.Equal(t, singledispatch.StateCanResend, lastMarker(t, context).State)

			context.AdvanceAttempt()
			result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
			require.NoError(t, err)
			require.Equal(t, oauth2http.PostNonIdempotentBranchCompleted, result.Branch)
			require.Len(t, session.server.requests(), 2)
		})
	}
}

func TestPostNonIdempotentRetriesAPOSTThatNeverLeftTheProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	closedAddress := listener.Addr().String()
	require.NoError(t, listener.Close())
	session := newTestSession(t, func(config *oauth2http.Config) { config.APIBaseURL = "http://" + closedAddress }, time.Hour)
	context := singledispatchtest.NewContext("oauth2-http-flow", "refused")

	_, err = postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders"})

	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Equal(t, singledispatch.StateCanResend, lastMarker(t, context).State)
}

func TestPostNonIdempotentRenewsTheTokenAfterA401AndSendsOnTheNextAttempt(t *testing.T) {
	session := newTestSession(t, nil, 10*time.Minute)
	session.server.expireAccessToken("access-token-1")
	session.server.onUnauthorized = func() { session.clock.advance(11 * time.Minute) }
	context := singledispatchtest.NewContext("oauth2-http-flow", "renewed")

	_, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "the POST is not resent inside the attempt that sent it")
	require.Len(t, session.server.requests(), 1)

	context.AdvanceAttempt()
	result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders", Body: "{}"})
	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchCompleted, result.Branch)
	requests := session.server.requests()
	require.Len(t, requests, 2)
	require.NotEqual(t, "Bearer access-token-1", requests[1].authorization, "the next attempt uses a renewed token")
}

func TestPostNonIdempotentSelectsRejectedWhenA401CannotBeRenewed(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	session.server.expireAccessToken("access-token-1")

	result, err := postNonIdempotent(t, session, singledispatchtest.NewContext("oauth2-http-flow", "unrenewable"), oauth2http.PostNonIdempotentInput{Path: "/orders"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchRejected, result.Branch)
	require.Equal(t, http.StatusUnauthorized, result.Value.StatusCode)
	require.Len(t, session.server.requests(), 1)
}

func TestPostNonIdempotentRetriesWhenTheCheckpointCannotBeRead(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	context := singledispatchtest.NewContext("oauth2-http-flow", "unreadable")
	context.SetReadError(errors.New("heartbeat unavailable"))

	_, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders"})

	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, session.server.requests())
}

func TestPostNonIdempotentSelectsUncertainWithoutSendingAfterAnEarlierPossibleSend(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	context := singledispatchtest.NewContext("oauth2-http-flow", "earlier-send")
	// The second attempt sees no checkpoint: the first attempt's claim was lost with its Worker.
	context.AdvanceAttempt()

	result, err := postNonIdempotent(t, session, context, oauth2http.PostNonIdempotentInput{Path: "/orders"})

	require.NoError(t, err)
	require.Equal(t, oauth2http.PostNonIdempotentBranchUncertain, result.Branch)
	require.Empty(t, session.server.requests())
}

func TestPostNonIdempotentSelectsDefectWithoutSendingInvalidInput(t *testing.T) {
	session := newTestSession(t, nil, time.Hour)
	for name, input := range map[string]oauth2http.PostNonIdempotentInput{
		"neither path nor url": {},
		"relative path":        {Path: "orders"},
		"Authorization header": {Path: "/orders", Headers: map[string]string{"Authorization": "Bearer x"}},
	} {
		result, err := postNonIdempotent(t, session, singledispatchtest.NewContext("oauth2-http-flow", "invalid"), input)
		require.NoError(t, err, name)
		require.Equal(t, oauth2http.PostNonIdempotentBranchDefect, result.Branch, name)
		require.Contains(t, result.Failure.Message, "postNonIdempotent", name)
	}
	require.Empty(t, session.server.requests())
}

func postNonIdempotent(
	t *testing.T, session *testSession, context *singledispatchtest.Context, input oauth2http.PostNonIdempotentInput,
) (oauth2http.PostNonIdempotentResult, error) {
	t.Helper()
	return sdkgo.RunMutation(context, session.client.PostNonIdempotent(), testConnection, input)
}

// lastMarker decodes the last heartbeat the attempt recorded.
func lastMarker(t *testing.T, context *singledispatchtest.Context) singledispatch.Marker {
	t.Helper()
	frames := context.Heartbeats()
	require.NotEmpty(t, frames)
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(frames[len(frames)-1], &marker))
	return marker
}
