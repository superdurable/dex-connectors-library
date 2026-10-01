// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func replyToConversation(t *testing.T, client *helpscout.Client, execution *stepContext, input helpscout.ReplyToConversationInput) (helpscout.ReplyToConversationResult, error) {
	t.Helper()
	return sdkgo.RunMutation(execution, client.ReplyToConversation(), testConnection, input)
}

func createdThread(threadID string) http.HandlerFunc {
	return respondStatus(http.StatusCreated, map[string]string{"Resource-Id": threadID})
}

func TestReplyToConversationRepliesToThePrimaryCustomerOnceAndNamesTheThread(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501":        respondJSON(http.StatusOK, conversationJSON(501, "active", 123)),
		"POST /v2/conversations/501/reply": createdThread("567"),
	})
	execution := newStepContext("reply")
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "We refunded the duplicate charge.", Status: helpscout.ConversationStatusPending,
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, helpscout.ConversationReply{ConversationID: 501, ThreadID: 567, CustomerID: 238604}, result.Value)
	require.Equal(t, "567", result.Receipt.ProviderObjectID)
	posts := fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply")
	require.Len(t, posts, 1)
	require.JSONEq(t, `{"customer":{"id":238604},"text":"We refunded the duplicate charge.","status":"pending"}`, posts[0].body)
	require.Equal(t, "application/json; charset=UTF-8", posts[0].contentType)
	require.True(t, execution.hasHeartbeat(), "the dispatch checkpoint stays until the Step completes")
	requireSecretFree(t, result)
}

func TestReplyToConversationAddsAnInternalNoteWithoutReadingTheConversation(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": createdThread("568")})
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), newStepContext("note"), helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Customer is on the Pro plan.", IsInternalNote: true,
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, helpscout.ConversationReply{ConversationID: 501, ThreadID: 568, IsInternalNote: true}, result.Value)
	require.Len(t, fake.recordedRequests(), 1)
	require.JSONEq(t, `{"text":"Customer is on the Pro plan."}`, fake.recordedRequests()[0].body)

	withCustomer := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/reply": createdThread("not-a-number")})
	unnamed, err := replyToConversation(t, newFakeBackedClient(t, withCustomer), newStepContext("reply"), helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Hello", CustomerID: 42,
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, unnamed.Branch, "a 201 means the thread exists")
	require.Zero(t, unnamed.Value.ThreadID)
	require.Len(t, withCustomer.recordedRequests(), 1, "an explicit customer needs no read")
}

func TestReplyToConversationNeverResendsAfterAnEarlierDispatch(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": createdThread("568")})
	execution := newStepContext("reply")
	require.NoError(t, execution.RecordHeartbeat(map[string]bool{"isHelpScoutThreadDispatched": true}))
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Contains(t, result.Failure.Message, "not sent again")
	require.Empty(t, fake.recordedRequests())
}

func TestReplyToConversationRetriesOnlyWhatHelpScoutProvablyDidNotApply(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.HandlerFunc
		isRetry bool
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
	}{
		{name: "rate limit", handler: func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("X-RateLimit-Retry-After", "3")
			writeJSON(response, http.StatusTooManyRequests, helpScoutErrorBody("Too many requests", nil))
		}, isRetry: true, kind: sdkgo.FailureRateLimit},
		{name: "documented safe 504", handler: respondJSON(http.StatusGatewayTimeout, helpScoutErrorBody("Internal Timeout", nil)),
			isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "server error", handler: respondJSON(http.StatusInternalServerError, helpScoutErrorBody("Ooops", nil)),
			branch: sdkgo.UncertainBranchID, kind: sdkgo.FailureAvailability},
		{name: "unavailable", handler: respondJSON(http.StatusServiceUnavailable, helpScoutErrorBody("Unavailable", nil)),
			branch: sdkgo.UncertainBranchID, kind: sdkgo.FailureAvailability},
		{name: "lost answer", handler: dropConnection, branch: sdkgo.UncertainBranchID, kind: sdkgo.FailureTransport},
		{name: "locked conversation", handler: respondJSON(http.StatusPreconditionFailed, helpScoutErrorBody("Conversation Locked", nil)),
			branch: helpscout.ReplyToConversationBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "invalid status", handler: respondJSON(http.StatusBadRequest, helpScoutErrorBody("Bad request", map[string]string{"status": "EnumValue"})),
			branch: helpscout.ReplyToConversationBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "deleted conversation", handler: respondJSON(http.StatusNotFound, helpScoutErrorBody("Not Found", nil)),
			branch: helpscout.ReplyToConversationBranchNotFound, kind: sdkgo.FailureNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": test.handler})
			execution := newStepContext("reply")
			result, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
				ConversationID: 501, Text: "Note", IsInternalNote: true,
			})
			require.Len(t, fake.recordedRequests(), 1)
			if test.isRetry {
				requireRetry(t, err, test.kind)
				require.False(t, execution.hasHeartbeat(), "nothing was applied, so the next attempt may send")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireSecretFree(t, result)
			if test.branch == sdkgo.UncertainBranchID {
				require.True(t, execution.hasHeartbeat())
				require.Contains(t, result.Failure.Message, "may exist")
			}
		})
	}
}

func TestReplyToConversationRetriesWhenTheConnectionNeverOpened(t *testing.T) {
	refused := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	})}
	execution := newStepContext("reply")
	_, err := replyToConversation(t, newTestClient(t, refused, staticCredentials(""), helpscout.Config{}), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	requireRetry(t, err, sdkgo.FailureTransport)
	require.False(t, execution.hasHeartbeat())
}

func TestReplyToConversationRejectsInvalidInputAndAConversationWithoutACustomer(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501": respondJSON(http.StatusOK, strings.Replace(conversationJSON(501, "active", 123),
			`"primaryCustomer": {"id": 238604`, `"primaryCustomer": {"id": 0`, 1)),
		"GET /v2/conversations/502": respondStatus(http.StatusMovedPermanently, map[string]string{"Location": "/v2/conversations/777"}),
	})
	client := newFakeBackedClient(t, fake)
	for _, input := range []helpscout.ReplyToConversationInput{
		{Text: "Hello"},
		{ConversationID: 501, Text: "   "},
		{ConversationID: 501, Text: strings.Repeat("x", helpscout.MaxReplyTextBytes+1)},
		{ConversationID: 501, Text: "Hello", IsInternalNote: true, CustomerID: 42},
		{ConversationID: 501, Text: "Hello", Status: "open"},
		{ConversationID: 501, Text: "Hello", CustomerID: -1},
	} {
		result, err := replyToConversation(t, client, newStepContext("reply"), input)
		require.NoError(t, err)
		require.Equal(t, helpscout.ReplyToConversationBranchDefect, result.Branch, input)
	}
	require.Empty(t, fake.recordedRequests())

	result, err := replyToConversation(t, client, newStepContext("reply"), helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchProviderRejected, result.Branch)
	require.Contains(t, result.Failure.Message, "pass customerId")
	merged, err := replyToConversation(t, client, newStepContext("reply"), helpscout.ReplyToConversationInput{ConversationID: 502, Text: "Hello"})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchNotFound, merged.Branch)
	require.Contains(t, merged.Failure.Message, "merged into conversation 777")
	require.Empty(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"))
}

func TestReplyToConversationSendsNothingWhenTooLittleOfTheAttemptRemains(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": createdThread("568")})
	execution := newStepContext("reply")
	deadlineContext, cancel := contextWithTimeout(6 * time.Second)
	defer cancel()
	execution.Context = deadlineContext
	_, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, retry.Failure.Message, "nothing was sent")
	require.Empty(t, fake.recordedRequests())
	require.False(t, execution.hasHeartbeat())
}

func TestReplyToConversationRecordsTheCheckpointBeforeSending(t *testing.T) {
	execution := newStepContext("reply")
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes": func(response http.ResponseWriter, _ *http.Request) {
			var checkpoint map[string]bool
			isFound, err := execution.GetLastHeartbeatValue(&checkpoint)
			if err != nil || !isFound || !checkpoint["isHelpScoutThreadDispatched"] {
				writeJSON(response, http.StatusConflict, `{}`)
				return
			}
			createdThread("568")(response, nil)
		},
	})
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), sentinelToken)
}

// dropConnection closes the connection after the request was read, as a lost answer looks to the client.
func dropConnection(response http.ResponseWriter, _ *http.Request) {
	hijacker, isHijacker := response.(http.Hijacker)
	if !isHijacker {
		panic("the test server cannot drop connections")
	}
	connection, _, err := hijacker.Hijack()
	if err != nil {
		panic(err)
	}
	_ = connection.Close() // The client observes the close; the close error itself is irrelevant.
}

func contextWithTimeout(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}
