// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestReplyToConversationSendsAReplyThatKeepsTheConversationOpen(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusAccepted, `{"status":"accepted","message_uid":"1eab543f84a0785f7b6b8967cck18f4d"}`)
	})
	dexContext := newTestDexContext("reply")
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hi Jane,\n\nWe refunded <order> 88213.\nThanks", AuthorID: testTeammateID})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "1eab543f84a0785f7b6b8967cck18f4d", result.Value.MessageUID)
	require.Empty(t, result.Value.CommentID)
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/conversations/"+testConversationID+"/messages", request.path)
	require.JSONEq(t, `{"body":"<p>Hi Jane,</p><p>We refunded &lt;order&gt; 88213.<br>Thanks</p>","text":"Hi Jane,\n\nWe refunded <order> 88213.\nThanks",
		"author_id":"`+testTeammateID+`","options":{"archive":false}}`, request.body)
	require.True(t, dexContext.hasHeartbeat(), "the dispatch checkpoint stays after a sent reply")
}

func TestReplyToConversationAddsAnInternalComment(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2","body":"**Refund** already issued","is_pinned":false}`)
	})
	result, err := sdkgo.RunMutation(newTestDexContext("comment"), newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "**Refund** already issued", IsInternalNote: true})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "com_1ywg3f2", result.Value.CommentID)
	require.Equal(t, "com_1ywg3f2", result.Receipt.ProviderObjectID)
	require.Equal(t, "/conversations/"+testConversationID+"/comments", provider.request(0).path)
	require.JSONEq(t, `{"body":"**Refund** already issued"}`, provider.request(0).body)
}

func TestReplyToConversationNeverSendsAgainAfterAnEarlierDispatch(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	dexContext := newTestDexContext("reply-again")
	require.NoError(t, dexContext.RecordHeartbeat(map[string]bool{"isFrontReplyDispatched": true}))
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Contains(t, result.Failure.Message, "not sent again")
}

func TestReplyToConversationClassifiesEveryAnswer(t *testing.T) {
	for name, test := range map[string]struct {
		status             int
		location           string
		branch             sdkgo.BranchID
		isRetry            bool
		shouldKeepDispatch bool
	}{
		"rate limited":   {status: http.StatusTooManyRequests, isRetry: true, shouldKeepDispatch: true},
		"unavailable":    {status: http.StatusServiceUnavailable, isRetry: true, shouldKeepDispatch: true},
		"server error":   {status: http.StatusInternalServerError, branch: front.ReplyToConversationBranchUncertain, shouldKeepDispatch: true},
		"timeout status": {status: http.StatusRequestTimeout, branch: front.ReplyToConversationBranchUncertain, shouldKeepDispatch: true},
		"missing":        {status: http.StatusNotFound, branch: front.ReplyToConversationBranchNotFound, shouldKeepDispatch: true},
		"merged":         {status: http.StatusMovedPermanently, location: "https://api2.frontapp.com/conversations/cnv_yo1kg5q", branch: front.ReplyToConversationBranchNotFound, shouldKeepDispatch: true},
		"no send right":  {status: http.StatusForbidden, branch: front.ReplyToConversationBranchProviderRejected, shouldKeepDispatch: true},
		"bad author":     {status: http.StatusBadRequest, branch: front.ReplyToConversationBranchProviderRejected, shouldKeepDispatch: true},
		"conflict":       {status: http.StatusConflict, branch: front.ReplyToConversationBranchUncertain, shouldKeepDispatch: true},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.location != "" {
					response.Header().Set("Location", test.location)
				}
				writeFrontError(t, response, test.status)
			})
			dexContext := newTestDexContext("reply-" + name)
			result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
				front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
			require.Equal(t, test.shouldKeepDispatch, dexContext.hasHeartbeat())
			require.Equal(t, 1, provider.requestCount())
			if test.isRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.NotContains(t, result.Failure.Message, providerSentinel)
			if test.location != "" {
				require.Contains(t, result.Failure.Message, "cnv_yo1kg5q")
			}
		})
	}
}

func TestReplyToConversationRetriesWhenFrontCannotBeReached(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) {})
	baseURL := provider.URL
	provider.Close()
	dexContext := newTestDexContext("reply-unreachable")
	_, err := sdkgo.RunMutation(dexContext, newFrontClient(t, baseURL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.True(t, dexContext.hasHeartbeat(), "a request that never left records canResend")
}

func TestReplyToConversationFenceRetriesOnlyTheNextAttemptAfter503(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeFrontError(t, response, http.StatusServiceUnavailable)
			return
		}
		writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2"}`)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "reply-503")
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello", IsInternalNote: true}
	_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, "com_1ywg3f2", result.Value.CommentID)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, provider.request(0).body, provider.request(1).body)
}

func TestReplyToConversationFenceFailsClosedWithoutCurrentMarker(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no resend is allowed") })
	dexContext := singledispatchtest.NewContext("front-flow", "missing-marker")
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCount())

	require.NoError(t, dexContext.SetPersistedCheckpoint(singledispatch.Marker{
		CallID: "another-call", Attempt: 1, State: singledispatch.StateCanResend,
	}))
	result, err = sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Zero(t, provider.requestCount())
}

func TestReplyToConversationFenceReadsBackAcceptedCommentWithoutResending(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2"}`)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/comments/com_1ywg3f2", request.URL.Path)
		writeJSON(t, response, http.StatusOK, `{"id":"com_1ywg3f2"}`)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "accepted-comment")
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello", IsInternalNote: true}
	first, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, first.Branch)
	dexContext.AdvanceAttempt()
	second, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, second.Branch)
	require.Equal(t, "com_1ywg3f2", second.Value.CommentID)
	require.Equal(t, 2, provider.requestCount())
}

func TestReplyToConversationFenceRetriesCredentialFailureBeforeSending(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2"}`)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "credential-recovery")
	missingCredentials, err := front.New(front.Config{}, sdkgo.StaticCredentialProvider[front.Credentials]{}, front.WithAPIBaseURL(provider.URL))
	require.NoError(t, err)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello", IsInternalNote: true}
	_, err = sdkgo.RunMutation(dexContext, missingCredentials.ReplyToConversation(), frontConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, provider.requestCount())
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestReplyToConversationFenceRejectsStaleCanResend(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		require.Zero(t, index, "a stale marker must not permit another write")
		writeFrontError(t, response, http.StatusServiceUnavailable)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "stale-can-resend")
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"}
	_, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	dexContext.AdvanceAttempt()
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestReplyToConversationFenceNeverResendsAfterLostClaim(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		require.Zero(t, index, "a lost claim must not permit a second write")
		writeJSON(t, response, http.StatusAccepted, `{"status":"accepted"}`)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "lost-claim")
	dexContext.LoseNextHeartbeat()
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"}
	_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestReplyToConversationFenceRetriesReadBackFailureWithoutResending(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2"}`)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		writeFrontError(t, response, http.StatusServiceUnavailable)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "read-back-failure")
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello", IsInternalNote: true}
	_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	dexContext.AdvanceAttempt()
	_, err = sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestReplyToConversationFenceSelectsUncertainWhenReadBackCannotConfirm(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusCreated, `{"id":"com_1ywg3f2"}`)
			return
		}
		require.Equal(t, http.MethodGet, request.Method)
		writeFrontError(t, response, http.StatusUnauthorized)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "read-back-rejected")
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello", IsInternalNote: true}
	_, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	dexContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestReplyToConversationAcceptedWithoutIDNeverResends(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		require.Zero(t, index, "the accepted write must not be repeated")
		writeJSON(t, response, http.StatusAccepted, `{"status":"accepted"}`)
	})
	dexContext := singledispatchtest.NewContext("front-flow", "accepted-without-id")
	client := newFrontClient(t, provider.URL)
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"}
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	dexContext.AdvanceAttempt()
	result, err = sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestReplyToConversationUnusableAcceptedBodyNeverResends(t *testing.T) {
	for name, test := range map[string]struct {
		body             string
		maxResponseBytes int64
	}{
		"unparsable": {body: "{invalid"},
		"oversized":  {body: `{"status":"accepted"}`, maxResponseBytes: 8},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				require.Zero(t, index, "an accepted write with an unusable body must not repeat")
				writeJSON(t, response, http.StatusAccepted, test.body)
			})
			client, err := front.New(front.Config{MaxResponseBytes: test.maxResponseBytes}, testCredentialProvider(), front.WithAPIBaseURL(provider.URL))
			require.NoError(t, err)
			dexContext := singledispatchtest.NewContext("front-flow", "unusable-"+name)
			input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"}
			result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
			require.NoError(t, err)
			require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
			dexContext.AdvanceAttempt()
			result, err = sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
			require.NoError(t, err)
			require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestReplyToConversationFenceStopsMidRequestBeforeAttemptDeadline(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	dexContext := singledispatchtest.NewContext("front-flow", "mid-request-deadline")
	cancel := dexContext.SetDeadline(time.Now().Add(300 * time.Millisecond))
	defer cancel()
	result, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

type injectIdempotencyKeyTransport struct{ base http.RoundTripper }

func (transport injectIdempotencyKeyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		request.Header.Set("Idempotency-Key", "same-call")
	}
	return transport.base.RoundTrip(request)
}

func TestReplyToConversationFencedWriteDisablesHTTPReplay(t *testing.T) {
	var postCount atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			response.WriteHeader(http.StatusOK)
			return
		}
		postCount.Add(1)
		_, err := io.Copy(io.Discard, request.Body)
		require.NoError(t, err)
		connection, _, err := http.NewResponseController(response).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer provider.Close()
	baseTransport := &http.Transport{}
	defer baseTransport.CloseIdleConnections()
	httpClient := &http.Client{Transport: injectIdempotencyKeyTransport{base: baseTransport}}
	prime, err := httpClient.Get(provider.URL + "/prime")
	require.NoError(t, err)
	require.NoError(t, prime.Body.Close())
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("front-flow", "http-replay"),
		newFrontClient(t, provider.URL, front.WithHTTPClient(httpClient)).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.EqualValues(t, 1, postCount.Load())
}

// untracedTransport drops the request's context, and with it the connector's dispatch trace.
type untracedTransport struct{ transport *http.Transport }

func (untraced untracedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return untraced.transport.RoundTrip(request.Clone(context.Background()))
}

func TestReplyToConversationSelectsUncertainWhenTheTransportHidesTheDispatch(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		connection, _, err := http.NewResponseController(response).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	transport := &http.Transport{}
	t.Cleanup(transport.CloseIdleConnections)
	client := newFrontClient(t, provider.URL, front.WithHTTPClient(&http.Client{Transport: untracedTransport{transport: transport}}))
	dexContext := newTestDexContext("reply-untraced")
	input := front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"}
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch, "Front received the POST, so its loss is not proof of no send")
	require.True(t, dexContext.hasHeartbeat())

	_, err = sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
	require.NoError(t, err)
	require.Equal(t, 1, provider.requestCount(), "a later attempt never sends again")
}

func TestReplyToConversationSelectsUncertainWhenTheAnswerIsTooLate(t *testing.T) {
	release := make(chan struct{})
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		<-release
		writeJSON(t, response, http.StatusAccepted, `{"status":"accepted"}`)
	})
	defer close(release)
	dexContext := newTestDexContext("reply-slow")
	client := newFrontClient(t, provider.URL, front.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, front.ReplyToConversationBranchUncertain, result.Branch)
	require.True(t, dexContext.hasHeartbeat())
}

func TestReplyToConversationSendsNothingWithoutTheCheckpoint(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	dexContext := newTestDexContext("reply-no-checkpoint")
	dexContext.recordErr = errors.New("heartbeat unavailable")
	_, err := sdkgo.RunMutation(dexContext, newFrontClient(t, provider.URL).ReplyToConversation(), frontConnection,
		front.ReplyToConversationInput{ConversationID: testConversationID, Text: "Hello"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestReplyToConversationValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newFrontClient(t, provider.URL)
	for name, input := range map[string]front.ReplyToConversationInput{
		"blank ID":           {Text: "Hello"},
		"blank text":         {ConversationID: testConversationID, Text: " \n"},
		"long text":          {ConversationID: testConversationID, Text: string(make([]byte, front.MaxReplyTextBytes+1))},
		"archived comment":   {ConversationID: testConversationID, Text: "Hello", IsInternalNote: true, ShouldArchive: true},
		"author email":       {ConversationID: testConversationID, Text: "Hello", AuthorID: "leela@planet-express.example.com"},
		"conversation alias": {ConversationID: "alt:ref:abc", Text: "Hello"},
	} {
		dexContext := newTestDexContext("reply-" + name)
		result, err := sdkgo.RunMutation(dexContext, client.ReplyToConversation(), frontConnection, input)
		require.NoError(t, err)
		require.Equal(t, front.ReplyToConversationBranchDefect, result.Branch, name)
		require.False(t, dexContext.hasHeartbeat(), name)
	}
}

func TestBuildReplyHTMLEscapesAndKeepsLineBreaks(t *testing.T) {
	require.Equal(t, "<p>a &amp; b</p><p>c<br>d</p>", front.BuildReplyHTML("a & b\r\n\r\nc\nd\n\n\n"))
	require.Equal(t, "<p>&lt;script&gt;</p>", front.BuildReplyHTML("<script>"))
}
