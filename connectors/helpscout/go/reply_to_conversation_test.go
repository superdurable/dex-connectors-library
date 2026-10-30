// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/go"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

func replyToConversation(t *testing.T, client *helpscout.Client, execution dex.Context, input helpscout.ReplyToConversationInput) (helpscout.ReplyToConversationResult, error) {
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
	require.Equal(t, sdkgo.UncertainBranchID, unnamed.Branch, "a 201 without a thread ID cannot be confirmed after interruption")
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
		{name: "service unavailable", handler: respondJSON(http.StatusServiceUnavailable, helpScoutErrorBody("Unavailable", nil)),
			isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "server error", handler: respondJSON(http.StatusInternalServerError, helpScoutErrorBody("Ooops", nil)),
			branch: sdkgo.UncertainBranchID, kind: sdkgo.FailureAvailability},
		{name: "conflict", handler: respondJSON(http.StatusConflict, helpScoutErrorBody("Conflict", nil)),
			branch: sdkgo.UncertainBranchID, kind: sdkgo.FailureConflict},
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
				require.True(t, execution.hasHeartbeat(), "the fence records permission for only the next attempt")
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
	refused := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		httptrace.ContextClientTrace(request.Context()).ConnectDone("tcp", "api.helpscout.net:443", syscall.ECONNREFUSED)
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
	})}
	execution := newStepContext("reply")
	_, err := replyToConversation(t, newTestClient(t, refused, staticCredentials(""), helpscout.Config{}), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	requireRetry(t, err, sdkgo.FailureTransport)
	require.True(t, execution.hasHeartbeat())
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
	deadlineContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	execution.Context = deadlineContext
	_, err := replyToConversation(t, newFakeBackedClient(t, fake), execution, helpscout.ReplyToConversationInput{
		ConversationID: 501, Text: "Note", IsInternalNote: true,
	})
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, retry.Failure.Message, "not sent")
	require.Empty(t, fake.recordedRequests())
	require.True(t, execution.hasHeartbeat())
}

func TestReplyToConversationRecordsTheCheckpointBeforeSending(t *testing.T) {
	execution := newStepContext("reply")
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes": func(response http.ResponseWriter, _ *http.Request) {
			var checkpoint singledispatch.Marker
			isFound, err := execution.GetLastHeartbeatValue(&checkpoint)
			if err != nil || !isFound || checkpoint.State != singledispatch.StateClaimed {
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

func TestReplyFenceRetries503OnceWithTheSameCall(t *testing.T) {
	var posts atomic.Int32
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes": func(response http.ResponseWriter, _ *http.Request) {
			if posts.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			createdThread("568")(response, nil)
		},
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	_, err := replyToConversation(t, client, step, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	step.AdvanceAttempt()
	result, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.EqualValues(t, 2, posts.Load())
}

func TestReplyFenceReadsBackAcceptedThreadWithoutResending(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes": createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK,
			threadPageJSON([]string{threadJSON(568, "note", "Triage note", "2026-09-30T12:00:00Z")}, false)),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	step.AdvanceAttempt()
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, second.Branch)
	require.EqualValues(t, 568, second.Value.ThreadID)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFenceReadBackResolvesThePrimaryReplyCustomerWithoutResending(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501":        respondJSON(http.StatusOK, conversationJSON(501, "active", 123)),
		"POST /v2/conversations/501/reply": createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK,
			threadPageJSON([]string{threadJSON(568, "reply", "Response to Jane", "2026-09-30T12:00:00Z")}, false)),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Response to Jane"}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	require.EqualValues(t, 238604, first.Value.CustomerID)
	step.AdvanceAttempt()
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, second.Branch)
	require.EqualValues(t, 238604, second.Value.CustomerID)
	require.EqualValues(t, 568, second.Value.ThreadID)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501"), 1)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFenceReadBackUsesConversationWhenConfirmedThreadOmitsCustomer(t *testing.T) {
	var conversationReads atomic.Int32
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501": func(response http.ResponseWriter, request *http.Request) {
			conversationReads.Add(1)
			respondJSON(http.StatusOK, conversationJSON(501, "active", 123))(response, request)
		},
		"POST /v2/conversations/501/reply": createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK,
			threadPageJSON([]string{`{"id":568,"type":"reply"}`}, false)),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Response to Jane"}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	step.AdvanceAttempt()
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, second.Branch)
	require.Equal(t, helpscout.ConversationReply{ConversationID: 501, ThreadID: 568, CustomerID: 238604}, second.Value)
	require.EqualValues(t, 2, conversationReads.Load())
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFenceReadBackReturnsConfirmedThreadWithUnknownCustomer(t *testing.T) {
	var conversationReads atomic.Int32
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501": func(response http.ResponseWriter, request *http.Request) {
			if conversationReads.Add(1) == 1 {
				respondJSON(http.StatusOK, conversationJSON(501, "active", 123))(response, request)
				return
			}
			response.WriteHeader(http.StatusServiceUnavailable)
		},
		"POST /v2/conversations/501/reply": createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK,
			threadPageJSON([]string{`{"id":568,"type":"reply"}`}, false)),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Response to Jane"}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	step.AdvanceAttempt()
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, second.Branch)
	require.Equal(t, helpscout.ConversationReply{ConversationID: 501, ThreadID: 568}, second.Value)
	require.EqualValues(t, 2, conversationReads.Load())
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFenceTerminalCredentialsSelectDefectWithoutSending(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/oauth2/token": respondJSON(http.StatusUnauthorized, `{"error":"invalid_client"}`),
	})
	credentials := testsupport.NewRefreshingCredentialSource(helpscout.Credentials{
		AppID: "app-id", AppSecret: sdkgo.NewSecretString("app-secret"),
	}, nil)
	client := newRefreshingClient(t, credentials, fake.redirectingClient())
	result, err := replyToConversation(t, client, singledispatchtest.NewContext("helpscout-test-flow", "reply"),
		helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/oauth2/token"), 1)
	require.Empty(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"))
}

func TestReplyFenceUnusableTokenSelectsDefectWithoutSending(t *testing.T) {
	fake := newFakeHelpScout(t, nil)
	credentials := staticCredentials("")
	stored := credentials[testConnection]
	stored.AccessToken = sdkgo.NewSecretString("invalid\ntoken")
	credentials[testConnection] = stored
	result, err := replyToConversation(t, newTestClient(t, fake.redirectingClient(), credentials, helpscout.Config{}),
		singledispatchtest.NewContext("helpscout-test-flow", "reply"),
		helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, fake.recordedRequests())
}

func TestReplyFenceChecksAcceptedWriteBeforeCredentialResolution(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": createdThread("568")})
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	step.AdvanceAttempt()
	clientWithoutCredentials := newTestClient(t, fake.redirectingClient(), sdkgo.StaticCredentialProvider[helpscout.Credentials]{}, helpscout.Config{})
	_, err = replyToConversation(t, clientWithoutCredentials, step, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
}

func TestReplyFenceFailsClosedWhenTheClaimIsMissingOnALaterAttempt(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": dropConnection})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, first.Branch)
	step.AdvanceAttempt()
	require.NoError(t, step.SetPersistedCheckpoint(nil))
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, second.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
}

func TestReplyFenceRecordsNotSentAfterCredentialFailure(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"POST /v2/conversations/501/notes": createdThread("568")})
	missingCredentials := sdkgo.StaticCredentialProvider[helpscout.Credentials]{}
	clientWithoutCredentials := newTestClient(t, fake.redirectingClient(), missingCredentials, helpscout.Config{})
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	_, err := replyToConversation(t, clientWithoutCredentials, step, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	step.AdvanceAttempt()
	result, err := replyToConversation(t, newFakeBackedClient(t, fake), step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
}

func TestReplyFenceRecordsNotSentAfterPrimaryCustomerReadRetry(t *testing.T) {
	var reads atomic.Int32
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501": func(response http.ResponseWriter, _ *http.Request) {
			if reads.Add(1) == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeJSON(response, http.StatusOK, conversationJSON(501, "active", 123))
		},
		"POST /v2/conversations/501/reply": createdThread("568"),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Response to Jane"}
	_, err := replyToConversation(t, client, step, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	step.AdvanceAttempt()
	result, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.EqualValues(t, 2, reads.Load())
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/reply"), 1)
}

func TestReplyFenceCutsOffAnInFlightWriteWithoutResending(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes": func(response http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		},
	})
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	cancel := step.SetDeadline(time.Now().Add(400 * time.Millisecond))
	defer cancel()
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	client := newFakeBackedClient(t, fake)
	result, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	step.AdvanceAttempt()
	result, err = replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
}

func TestReplyFenceAcceptedBodyReadFailureKeepsTheThreadID(t *testing.T) {
	client := newTestClient(t, &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost {
			t.Fatal("unexpected provider request", request.Method)
		}
		require.Nil(t, request.GetBody)
		return &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"Resource-Id": {"568"}},
			Body: io.NopCloser(io.MultiReader(strings.NewReader("partial"), failingReader{}))}, nil
	})}, staticCredentials(""), helpscout.Config{})
	result, err := replyToConversation(t, client, singledispatchtest.NewContext("helpscout-test-flow", "reply"),
		helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true})
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, result.Branch)
	require.EqualValues(t, 568, result.Value.ThreadID)
}

func TestReplyFenceUnusableReadBackStaysUncertainWithoutAnotherPost(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes":  createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK, `{broken`),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	step.AdvanceAttempt()
	second, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, second.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFenceRetriesTransientReadBackWithoutAnotherPost(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"POST /v2/conversations/501/notes":  createdThread("568"),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusServiceUnavailable, `{}`),
	})
	client := newFakeBackedClient(t, fake)
	step := singledispatchtest.NewContext("helpscout-test-flow", "reply")
	input := helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true}
	first, err := replyToConversation(t, client, step, input)
	require.NoError(t, err)
	require.Equal(t, helpscout.ReplyToConversationBranchReplied, first.Branch)
	step.AdvanceAttempt()
	_, err = replyToConversation(t, client, step, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
	require.Len(t, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads"), 1)
}

func TestReplyFencePreventsTransportReplayOnAReusedConnection(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /prime":                       respondStatus(http.StatusNoContent, nil),
		"POST /v2/conversations/501/notes": dropConnection,
	})
	base := fake.redirectingClient().Transport
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			require.Nil(t, request.GetBody)
			request.Header.Set("Idempotency-Key", "same-call")
		}
		return base.RoundTrip(request)
	})}
	prime, err := httpClient.Get(fake.URL + "/prime")
	require.NoError(t, err)
	require.NoError(t, prime.Body.Close())
	client := newTestClient(t, httpClient, staticCredentials(""), helpscout.Config{})
	result, err := replyToConversation(t, client, singledispatchtest.NewContext("helpscout-test-flow", "reply"),
		helpscout.ReplyToConversationInput{ConversationID: 501, Text: "Triage note", IsInternalNote: true})
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Len(t, fake.requestsTo(http.MethodPost, "/v2/conversations/501/notes"), 1)
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
