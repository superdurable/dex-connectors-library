// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

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

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type failingOnceSlackCredentials struct {
	attempts atomic.Int32
}

func (provider *failingOnceSlackCredentials) Resolve(call sdkgo.Call) (slack.Credentials, error) {
	if provider.attempts.Add(1) == 1 {
		return slack.Credentials{}, errors.New("credential store unavailable")
	}
	return sdkgo.StaticCredentialProvider[slack.Credentials]{slackConnection: {
		BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"),
	}}.Resolve(call)
}

func (provider *failingOnceSlackCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[slack.Credentials]) (slack.Credentials, error) {
	return provider.Resolve(call)
}

func TestSharedFenceRetries503ExactlyOnce(t *testing.T) {
	var posts atomic.Int32
	var messageIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		messageIDs = append(messageIDs, payload["client_msg_id"])
		if posts.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"ok":false,"error":"service_unavailable"}`))
			return
		}
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-503", "post")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	_, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.Error(t, err)
	require.EqualValues(t, 1, posts.Load())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.EqualValues(t, 2, posts.Load())
	require.Len(t, messageIDs, 2)
	require.NotEmpty(t, messageIDs[0])
	require.Equal(t, messageIDs[0], messageIDs[1])
}

func TestSharedFenceCredentialFailureRecordsNotSent(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		posts.Add(1)
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	provider := &failingOnceSlackCredentials{}
	client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("shared-fence-credentials", "post")
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	_, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.Error(t, err)
	require.Zero(t, posts.Load())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.EqualValues(t, 1, posts.Load())
}

func TestSharedFenceLostClaimNeverResends(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		posts.Add(1)
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = response.Write([]byte(`{"ok":false,"error":"fatal_error"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-lost-claim", "post")
	ctx.LoseNextHeartbeat()
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	require.EqualValues(t, 1, posts.Load())
}

func TestSharedFenceAcceptedPostReadsBackWithoutResend(t *testing.T) {
	var posts atomic.Int32
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts.Add(1)
			_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
			return
		}
		reads.Add(1)
		require.Equal(t, "2.0", request.URL.Query().Get("oldest"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","user":"UBOT","text":"hello"}]}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-readback", "post")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.EqualValues(t, 1, posts.Load())
	require.EqualValues(t, 1, reads.Load())
}

func TestSharedFencePreparesSingleUseHTTPWrite(t *testing.T) {
	requests := 0
	client := newSlackClientWithTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Nil(t, request.GetBody)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"channel":"C123","ts":"2.0"}`)),
		}, nil
	}))
	ctx := singledispatchtest.NewContext("shared-fence-http", "post")
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 1, requests)
}
