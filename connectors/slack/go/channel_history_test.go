// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestListChannelMessagesSendsBoundedHistoryQueryWithBotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/conversations.history", request.URL.Path)
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.Equal(t, url.Values{
			"channel": {"C123"}, "oldest": {"1710000000.000100"}, "latest": {"1710000099.000100"},
			"inclusive": {"true"}, "limit": {"10"}, "cursor": {"page-2"},
		}, request.URL.Query())
		response.Header().Set("X-Slack-Req-Id", "history-1")
		_, _ = response.Write([]byte(`{"ok":true,"messages":[` +
			`{"type":"message","ts":"1710000030.000900","user":"U2","text":"Is order 1042 on track?"},` +
			`{"type":"message","ts":"1710000010.000500","thread_ts":"1710000010.000500","reply_count":2,"user":"UBOT","bot_id":"B123","text":"ref:order-1042"},` +
			`{"type":"message","subtype":"bot_message","ts":"1710000005.000300","bot_id":"B999","text":"Deploy finished"}` +
			`],"has_more":true,"response_metadata":{"next_cursor":"page-3"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-channel-history"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{
		ChannelID: "C123", OldestTimestamp: "1710000000.000100", LatestTimestamp: "1710000099.000100", IsInclusive: true, PageSize: 10, Cursor: "page-2",
	})
	require.NoError(t, err)
	require.Equal(t, slack.ListChannelMessagesBranchRead, result.Branch)
	require.Equal(t, slack.ListChannelMessagesOutput{Messages: []slack.ChannelMessage{
		{ChannelID: "C123", Timestamp: "1710000030.000900", UserID: "U2", Text: "Is order 1042 on track?"},
		{ChannelID: "C123", Timestamp: "1710000010.000500", ThreadTimestamp: "1710000010.000500", UserID: "UBOT", BotID: "B123", Text: "ref:order-1042", ReplyCount: 2},
		{ChannelID: "C123", Timestamp: "1710000005.000300", BotID: "B999", Text: "Deploy finished"},
	}, NextCursor: "page-3"}, result.Value)
	require.Equal(t, "history-1", result.Receipt.ProviderRequestID)
}

func TestListChannelMessagesFollowsCursorsUntilTheHistoryEnds(t *testing.T) {
	var requestedCursors []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		require.Equal(t, "15", query.Get("limit"))
		require.False(t, query.Has("oldest") || query.Has("latest") || query.Has("inclusive"))
		requestedCursors = append(requestedCursors, query.Get("cursor"))
		switch query.Get("cursor") {
		case "":
			_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"3.0","user":"U1","text":"newest"}],"has_more":true,"response_metadata":{"next_cursor":"cursor-2"}}`))
		case "cursor-2":
			_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"2.0","user":"U1","text":"middle"}],"has_more":true,"response_metadata":{"next_cursor":"cursor-3"}}`))
		default:
			_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"1.0","user":"U1","text":"oldest"}],"has_more":false,"response_metadata":{"next_cursor":""}}`))
		}
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	var texts []string
	cursor := ""
	for page := 0; page < 5; page++ {
		result, err := sdkgo.RunQuery(newSlackDexContext("history-page"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123", Cursor: cursor})
		require.NoError(t, err)
		require.Equal(t, slack.ListChannelMessagesBranchRead, result.Branch)
		for _, message := range result.Value.Messages {
			texts = append(texts, message.Text)
		}
		cursor = result.Value.NextCursor
		if cursor == "" {
			break
		}
	}
	require.Equal(t, []string{"", "cursor-2", "cursor-3"}, requestedCursors)
	require.Equal(t, []string{"newest", "middle", "oldest"}, texts)
}

func TestListChannelMessagesSelectsTerminalBranches(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		body        string
		branch      sdkgo.BranchID
		failureKind sdkgo.FailureKind
	}{
		{"not in channel", http.StatusOK, `{"ok":false,"error":"not_in_channel"}`, slack.ListChannelMessagesBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"channel not found", http.StatusOK, `{"ok":false,"error":"channel_not_found"}`, slack.ListChannelMessagesBranchProviderRejected, sdkgo.FailureNotFound},
		{"missing scope", http.StatusForbidden, `{"ok":false,"error":"missing_scope"}`, slack.ListChannelMessagesBranchProviderRejected, sdkgo.FailureAuthorization},
		{"invalid cursor", http.StatusOK, `{"ok":false,"error":"invalid_cursor"}`, slack.ListChannelMessagesBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"malformed JSON", http.StatusOK, `{"ok":`, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureProtocol},
		{"empty body", http.StatusOK, ``, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureProtocol},
		{"wrong reply count type", http.StatusOK, `{"ok":true,"messages":[{"ts":"1.0","text":"x","reply_count":"2"}]}`, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureProtocol},
		{"wrong bot ID type", http.StatusOK, `{"ok":true,"messages":[{"ts":"1.0","text":"x","bot_id":7}]}`, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureProtocol},
		{"missing timestamp", http.StatusOK, `{"ok":true,"messages":[{"user":"U1","text":"x"}]}`, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureProtocol},
		{"oversized body", http.StatusOK, `{"ok":true,"messages":[{"ts":"1.0","text":"` + strings.Repeat("x", 128) + `"}]}`, slack.ListChannelMessagesBranchInvalidResponse, sdkgo.FailureResponseTooLarge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()
			client, err := slack.New(slack.Config{Endpoint: server.URL, MaxResponseBytes: 100}, sdkgo.StaticCredentialProvider[slack.Credentials]{
				slackConnection: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")},
			})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newSlackDexContext("history-branch"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.failureKind, result.Failure.Kind)
			require.Empty(t, result.Value.Messages)
			require.Equal(t, int32(1), requests.Load())
		})
	}
}

func TestListChannelMessagesRetriesRateLimitsServerFailuresAndTransportErrors(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		retryAfter  string
		failureKind sdkgo.FailureKind
		delay       time.Duration
	}{
		{name: "429 with Retry-After", status: http.StatusTooManyRequests, retryAfter: "60", failureKind: sdkgo.FailureRateLimit, delay: time.Minute},
		{name: "500", status: http.StatusInternalServerError, failureKind: sdkgo.FailureAvailability},
		{name: "503 with Retry-After", status: http.StatusServiceUnavailable, retryAfter: "5", failureKind: sdkgo.FailureAvailability, delay: 5 * time.Second},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if testCase.retryAfter != "" {
					response.Header().Set("Retry-After", testCase.retryAfter)
				}
				response.WriteHeader(testCase.status)
			}))
			defer server.Close()
			client := newSlackClient(t, server.URL)
			_, err := sdkgo.RunQuery(newSlackDexContext("history-retry"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, testCase.failureKind, retry.Failure.Kind)
			var delayed *dex.RetryAfterError
			if testCase.delay == 0 {
				require.False(t, errors.As(err, &delayed))
			} else {
				require.ErrorAs(t, err, &delayed)
				require.Equal(t, testCase.delay, delayed.After)
			}
			require.Equal(t, int32(1), requests.Load())
		})
	}
	t.Run("transport failure", func(t *testing.T) {
		client := newSlackClientWithTransport(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("connection reset by peer")
		}))
		_, err := sdkgo.RunQuery(newSlackDexContext("history-transport"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	})
}

func TestListChannelMessagesRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input slack.ListChannelMessagesInput
	}{
		{"blank channel", slack.ListChannelMessagesInput{ChannelID: " "}},
		{"page size above Slack limit", slack.ListChannelMessagesInput{ChannelID: "C123", PageSize: 16}},
		{"negative page size", slack.ListChannelMessagesInput{ChannelID: "C123", PageSize: -1}},
		{"oldest is not a Slack timestamp", slack.ListChannelMessagesInput{ChannelID: "C123", OldestTimestamp: "yesterday"}},
		{"latest is not a Slack timestamp", slack.ListChannelMessagesInput{ChannelID: "C123", LatestTimestamp: "1710000000."}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := newSlackClientWithTransport(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid input must not reach Slack")
				return nil, nil
			}))
			result, err := sdkgo.RunQuery(newSlackDexContext("history-defect"), client.ListChannelMessages(), slackConnection, testCase.input)
			require.NoError(t, err)
			require.Equal(t, slack.ListChannelMessagesBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	t.Run("missing bot token", func(t *testing.T) {
		client, err := slack.New(slack.Config{Endpoint: "http://127.0.0.1:1"}, sdkgo.StaticCredentialProvider[slack.Credentials]{
			slackConnection: {UserToken: sdkgo.NewSecretString("user-token")},
		})
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newSlackDexContext("history-no-bot"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123"})
		require.NoError(t, err)
		require.Equal(t, slack.ListChannelMessagesBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	})
}

func TestListChannelMessagesRefreshesTheBotTokenOnceAfterRejection(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if requests == 1 {
			require.Equal(t, "Bearer rejected-bot", request.Header.Get("Authorization"))
			_, _ = response.Write([]byte(`{"ok":false,"error":"token_expired"}`))
			return
		}
		require.Equal(t, "Bearer replacement-bot", request.Header.Get("Authorization"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"1.0","user":"UBOT","text":"posted"}]}`))
	}))
	t.Cleanup(server.Close)
	provider := &rejectionRefreshingCredentialProvider{}
	client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newSlackDexContext("history-refresh"), client.ListChannelMessages(), slackConnection, slack.ListChannelMessagesInput{ChannelID: "C123"})
	require.NoError(t, err)
	require.Equal(t, slack.ListChannelMessagesBranchRead, result.Branch)
	require.Equal(t, 2, requests)
	require.Equal(t, 1, provider.forcedRefreshes)
}
