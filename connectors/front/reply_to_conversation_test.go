// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
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
		"rate limited":   {status: http.StatusTooManyRequests, isRetry: true},
		"server error":   {status: http.StatusInternalServerError, branch: front.ReplyToConversationBranchUncertain, shouldKeepDispatch: true},
		"timeout status": {status: http.StatusRequestTimeout, branch: front.ReplyToConversationBranchUncertain, shouldKeepDispatch: true},
		"missing":        {status: http.StatusNotFound, branch: front.ReplyToConversationBranchNotFound, shouldKeepDispatch: true},
		"merged":         {status: http.StatusMovedPermanently, location: "https://api2.frontapp.com/conversations/cnv_yo1kg5q", branch: front.ReplyToConversationBranchNotFound, shouldKeepDispatch: true},
		"no send right":  {status: http.StatusForbidden, branch: front.ReplyToConversationBranchProviderRejected, shouldKeepDispatch: true},
		"bad author":     {status: http.StatusBadRequest, branch: front.ReplyToConversationBranchProviderRejected, shouldKeepDispatch: true},
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
	require.False(t, dexContext.hasHeartbeat(), "a request that never left clears the checkpoint")
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
