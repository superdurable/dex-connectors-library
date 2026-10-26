// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var slackConnection = sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}

type rejectionRefreshingCredentialProvider struct {
	forcedRefreshes int
}

func (*rejectionRefreshingCredentialProvider) Resolve(sdkgo.Call) (slack.Credentials, error) {
	return slack.Credentials{
		BotToken: sdkgo.NewSecretString("rejected-bot"), UserToken: sdkgo.NewSecretString("rejected-user"),
		AppToken: sdkgo.NewSecretString("app-token"),
	}, nil
}

// ResolveWithRefresh returns the tokens Resolve returns, which have not expired before Slack rejects them.
func (provider *rejectionRefreshingCredentialProvider) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[slack.Credentials],
) (slack.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *rejectionRefreshingCredentialProvider) ResolveAfterRejection(
	context.Context,
	sdkgo.Call,
	sdkgo.CredentialRefreshDriver[slack.Credentials],
) (slack.Credentials, error) {
	provider.forcedRefreshes++
	return slack.Credentials{
		BotToken: sdkgo.NewSecretString("replacement-bot"), UserToken: sdkgo.NewSecretString("replacement-user"),
		AppToken: sdkgo.NewSecretString("app-token"),
	}, nil
}

func TestListThreadMessagesUsesUserTokenAndReturnsCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/conversations.replies", request.URL.Path)
		require.Equal(t, "Bearer user-token", request.Header.Get("Authorization"))
		require.Equal(t, "C123", request.URL.Query().Get("channel"))
		require.Equal(t, "15", request.URL.Query().Get("limit"))
		response.Header().Set("X-Slack-Req-Id", "request-1")
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"root"},{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"reply"}],"response_metadata":{"next_cursor":"next"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-thread"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchRead, result.Branch)
	require.Len(t, result.Value.Messages, 2)
	require.Equal(t, "next", result.Value.NextCursor)
	require.Equal(t, "request-1", result.Receipt.ProviderRequestID)
}

func TestGetThreadReplyReturnsNotFoundForAnotherTimestamp(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "2.0", request.URL.Query().Get("oldest"))
		require.Equal(t, "true", request.URL.Query().Get("inclusive"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"3.0","thread_ts":"1.0","user":"U2","text":"other"}]}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchNotFound, result.Branch)
}

func TestGetThreadReplyReturnsExactReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","user":"U2","text":"approve"}]}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply-found"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchFound, result.Branch)
	require.Equal(t, "approve", result.Value.Message.Text)
}

func TestGetThreadReplyProviderRejectionUsesProviderRejectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"missing_scope"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("get-reply-rejected"), client.GetThreadReply(), slackConnection, slack.GetThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", ReplyTimestamp: "2.0"})
	require.NoError(t, err)
	require.Equal(t, slack.GetThreadReplyBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
}

func TestListThreadMessagesRefreshesAndRetriesOnceAfterTokenExpiry(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			require.Equal(t, "Bearer rejected-user", request.Header.Get("Authorization"))
			_, _ = response.Write([]byte(`{"ok":false,"error":"token_expired"}`))
			return
		}
		require.Equal(t, "Bearer replacement-user", request.Header.Get("Authorization"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"root"}]}`))
	}))
	t.Cleanup(server.Close)
	provider := &rejectionRefreshingCredentialProvider{}
	client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newSlackDexContext("refresh-after-expiry"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{
		ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15,
	})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchRead, result.Branch)
	require.Equal(t, 2, requests)
	require.Equal(t, 1, provider.forcedRefreshes)
}

func TestListThreadMessagesDoesNotLoopWhenReplacementIsExpired(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = response.Write([]byte(`{"ok":false,"error":"token_expired"}`))
	}))
	t.Cleanup(server.Close)
	provider := &rejectionRefreshingCredentialProvider{}
	client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newSlackDexContext("no-refresh-loop"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{
		ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15,
	})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, 2, requests)
	require.Equal(t, 1, provider.forcedRefreshes)
}

func TestListThreadMessagesInvalidResponseUsesInvalidResponseBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-thread-invalid-response"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 15})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func TestPostThreadReplyUsesBotTokenThreadAndStableClientMessageID(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0","message":{"ts":"2.0","thread_ts":"1.0","user":"UBOT","text":"Processing complete"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	stepContext, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	ctx := newSlackDexContext("reply-complete")
	ctx.Context = stepContext
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "Processing complete"})
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.Equal(t, "1.0", payload["thread_ts"])
	require.Equal(t, string(result.Receipt.IdempotencyKey), payload["client_msg_id"])
	require.NotEmpty(t, payload["client_msg_id"])
}

func TestPostChannelMessageOmitsThreadTimestamp(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"1.0","message":{"ts":"1.0","user":"UBOT","text":"new request"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-root"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "new request"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.NotContains(t, payload, "thread_ts")
	require.Equal(t, "1.0", result.Value.Message.Timestamp)
}

func TestPostMessageProviderRejectionUsesProviderRejectedBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-rejected"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C404", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
}

func TestPostMessageServerFailureIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = response.Write([]byte(`{"ok":false,"error":"fatal_error"}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-unknown"), client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
}

func TestPostMessageConflictIsUncertainWithoutVerifiedReplay(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusConflict)
		_, _ = response.Write([]byte(`{"ok":false,"error":"conflict"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunMutation(newSlackDexContext("post-conflict"), client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
}

func TestPostMessageServiceUnavailableRetriesBothOperations(t *testing.T) {
	for _, testCase := range []struct {
		operation    string
		statusCode   int
		responseBody string
	}{
		{operation: "channel", statusCode: http.StatusServiceUnavailable, responseBody: `{"ok":false,"error":"service_unavailable"}`},
		{operation: "reply", statusCode: http.StatusServiceUnavailable, responseBody: `{"ok":false,"error":"service_unavailable"}`},
		{operation: "channel", statusCode: http.StatusServiceUnavailable, responseBody: `{"ok":`},
		{operation: "reply", statusCode: http.StatusTooManyRequests, responseBody: `{"ok":`},
	} {
		t.Run(testCase.operation+"-"+strconv.Itoa(testCase.statusCode)+"-"+testCase.responseBody, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests++
				response.WriteHeader(testCase.statusCode)
				_, _ = response.Write([]byte(testCase.responseBody))
			}))
			t.Cleanup(server.Close)
			client := newSlackClient(t, server.URL)
			ctx := newSlackDexContext("post-503-" + testCase.operation)
			var err error
			if testCase.operation == "channel" {
				_, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
			} else {
				_, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
			}
			require.Error(t, err)
			require.Equal(t, 1, requests)
			require.NotEmpty(t, ctx.heartbeat)
		})
	}
}

func TestPostMessageRetriesServiceUnavailableWithSameCallID(t *testing.T) {
	requests := 0
	var messageIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		messageIDs = append(messageIDs, payload["client_msg_id"])
		if requests == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"ok":false,"error":"service_unavailable"}`))
			return
		}
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := newSlackDexContext("post-503-recovery")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	_, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.Error(t, err)
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.Equal(t, 2, requests)
	require.Equal(t, messageIDs[0], messageIDs[1])
	require.NotEmpty(t, messageIDs[0])
}

func TestPostMessageMissingOrStaleRetryCheckpointDoesNotResend(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		attempt   int32
		heartbeat []byte
	}{
		{name: "missing", attempt: 2},
		{name: "stale", attempt: 3, heartbeat: []byte(`{"callId":"old","attempt":1,"state":"canResend"}`)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { requests++ }))
			t.Cleanup(server.Close)
			client := newSlackClient(t, server.URL)
			ctx := newSlackDexContext("post-checkpoint-" + testCase.name)
			ctx.attempt = testCase.attempt
			ctx.heartbeat = testCase.heartbeat
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
			require.NoError(t, err)
			require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
			require.Equal(t, 0, requests)
		})
	}
}

func TestPostMessageFailedCheckpointRetriesWithoutDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := newSlackDexContext("post-checkpoint-failed")
	ctx.heartbeatError = errors.New("heartbeat unavailable")
	_, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.Error(t, err)
	require.Equal(t, 0, requests)
}

func TestPostMessageFailedDispatchClaimFailsClosedWithoutCheckpoint(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := newSlackDexContext("post-dispatch-checkpoint-failed")
	ctx.heartbeatError = errors.New("heartbeat unavailable")
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	_, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.Error(t, err)
	require.Equal(t, 0, requests)
	require.Empty(t, ctx.heartbeat)
	ctx.heartbeatError = nil
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	require.Equal(t, 0, requests)
}

func TestPostMessageProvenUnsentRetriesButAmbiguousTransportIsUncertain(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		trace      func(*httptrace.ClientTrace)
		wantsRetry bool
	}{
		{name: "dns_failure", trace: func(trace *httptrace.ClientTrace) {
			trace.DNSDone(httptrace.DNSDoneInfo{Err: errors.New("lookup failed")})
		}, wantsRetry: true},
		{name: "connect_failure", trace: func(trace *httptrace.ClientTrace) {
			trace.ConnectDone("tcp", "localhost:443", errors.New("connect failed"))
		}, wantsRetry: true},
		{name: "tls_failure", trace: func(trace *httptrace.ClientTrace) {
			trace.TLSHandshakeDone(tls.ConnectionState{}, errors.New("handshake failed"))
		}, wantsRetry: true},
		{name: "unknown_transport", trace: func(*httptrace.ClientTrace) {}},
		{name: "connection_obtained", trace: func(trace *httptrace.ClientTrace) {
			trace.GotConn(httptrace.GotConnInfo{})
			trace.ConnectDone("tcp", "localhost:443", errors.New("connection closed"))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := newSlackClientWithTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
				testCase.trace(httptrace.ContextClientTrace(request.Context()))
				return nil, errors.New("transport failed")
			}))
			ctx := newSlackDexContext("post-transport-" + testCase.name)
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
			if testCase.wantsRetry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
			}
		})
	}
}

func TestPostMessageAcceptedWithoutIdentityIsNotResent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = response.Write([]byte(`{"ok":true,"message":{"text":"hello"}}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := newSlackDexContext("post-accepted-invalid")
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
	ctx.attempt = 2
	_, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, 1, requests)
}

func TestPostMessageDeadlineCutsBeforeDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	stepContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	ctx := newSlackDexContext("post-deadline")
	ctx.Context = stepContext
	_, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.Error(t, err)
	require.Equal(t, 0, requests)
	require.Contains(t, string(ctx.heartbeat), `"state":"notSent"`)
	ctx.Context = context.Background()
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 1, requests)
}

func TestPostMessageBodyErrorsPreserveSafeRetryAndUnknownOutcome(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		wantsRetry bool
	}{
		{name: "service_unavailable", wantsRetry: true},
		{name: "ratelimited", wantsRetry: true},
		{name: "internal_error"},
		{name: "fatal_error"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requests := 0
			var messageIDs []string
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				requests++
				var payload map[string]string
				require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
				messageIDs = append(messageIDs, payload["client_msg_id"])
				response.Header().Set("Content-Type", "application/json")
				if requests == 1 {
					_, _ = response.Write([]byte(`{"ok":false,"error":"` + testCase.name + `"}`))
					return
				}
				_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
			}))
			t.Cleanup(server.Close)
			client := newSlackClient(t, server.URL)
			ctx := newSlackDexContext("post-body-error-" + testCase.name)
			input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
			firstResult, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
			if testCase.wantsRetry {
				require.Error(t, err)
				require.Contains(t, string(ctx.heartbeat), `"state":"canResend"`)
			} else {
				require.NoError(t, err)
				require.Equal(t, slack.PostThreadReplyBranchUncertain, firstResult.Branch)
			}
			ctx.attempt = 2
			secondResult, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
			require.NoError(t, err)
			if testCase.wantsRetry {
				require.Equal(t, slack.PostThreadReplyBranchSent, secondResult.Branch)
				require.Equal(t, 2, requests)
				require.Equal(t, messageIDs[0], messageIDs[1])
				require.NotEmpty(t, messageIDs[0])
			} else {
				require.Equal(t, slack.PostThreadReplyBranchUncertain, secondResult.Branch)
				require.Equal(t, 1, requests)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func newSlackClientWithTransport(t *testing.T, transport http.RoundTripper) *slack.Client {
	t.Helper()
	client, err := slack.New(slack.Config{}, sdkgo.StaticCredentialProvider[slack.Credentials]{
		slackConnection: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")},
	}, slack.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	return client
}

func TestListThreadMessagesRejectsPageLargerThanSlackLimit(t *testing.T) {
	client := newSlackClient(t, "http://127.0.0.1:1")
	result, err := sdkgo.RunQuery(newSlackDexContext("invalid-page"), client.ListThreadMessages(), slackConnection, slack.ListThreadMessagesInput{ChannelID: "C123", ThreadTimestamp: "1.0", PageSize: 16})
	require.NoError(t, err)
	require.Equal(t, slack.ListThreadMessagesBranchDefect, result.Branch)
}

func newSlackClient(t *testing.T, endpoint string) *slack.Client {
	t.Helper()
	client, err := slack.New(slack.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[slack.Credentials]{
		slackConnection: {
			BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token"),
		},
	})
	require.NoError(t, err)
	return client
}

type slackDexContext struct {
	context.Context
	step                 string
	attempt              int32
	heartbeat            []byte
	heartbeatError       error
	heartbeatErrorOnCall int
	heartbeatWriteCount  int
}

func newSlackDexContext(step string) *slackDexContext {
	return &slackDexContext{Context: context.Background(), step: step}
}

func (*slackDexContext) FlowID() string                        { return "slack-flow" }
func (*slackDexContext) RunID() string                         { return "run" }
func (*slackDexContext) FlowStartedAt() time.Time              { return time.Unix(1, 0) }
func (context *slackDexContext) StepExecutionID() string       { return context.step }
func (*slackDexContext) FromStepExecutionID() string           { return "" }
func (*slackDexContext) RecoveryError() *dex.RecoveryErrorInfo { return nil }
func (*slackDexContext) FirstAttemptAt() time.Time             { return time.Unix(1, 0) }
func (context *slackDexContext) Attempt() int32 {
	if context.attempt == 0 {
		return 1
	}
	return context.attempt
}
func (*slackDexContext) HasTimerFired() bool           { return false }
func (*slackDexContext) HasTimerFiredByIndex(int) bool { return false }
func (*slackDexContext) WaitForMethodFailed() bool     { return false }
func (context *slackDexContext) RecordHeartbeat(value any) error {
	context.heartbeatWriteCount++
	if context.heartbeatError != nil {
		return context.heartbeatError
	}
	if context.heartbeatWriteCount == context.heartbeatErrorOnCall {
		return errors.New("heartbeat unavailable")
	}
	if value == nil {
		context.heartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.heartbeat = encoded
	return nil
}
func (context *slackDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if len(context.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(context.heartbeat, target)
}
func (*slackDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*slackDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*slackDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*slackDexContext)(nil)
