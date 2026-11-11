// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListChannelsFindsNamedChannelWithBotToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/conversations.list", request.URL.Path)
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.Equal(t, "public_channel,private_channel", request.URL.Query().Get("types"))
		require.Equal(t, "true", request.URL.Query().Get("exclude_archived"))
		require.Equal(t, "200", request.URL.Query().Get("limit"))
		require.False(t, request.URL.Query().Has("cursor"))
		_, _ = response.Write([]byte(`{"ok":true,"channels":[` +
			`{"id":"C999","name":"random","is_member":true},` +
			`{"id":"C123","name":"general","is_member":true,"topic":{"value":"News"},"purpose":{"value":"Company-wide"}},` +
			`{"id":"G456","name":"general-leads","is_private":true,"is_member":false}` +
			`],"response_metadata":{"next_cursor":"next"}}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-channels"), client.ListChannels(), slackConnection, slack.ListChannelsInput{NameFilter: " #GENERAL "})
	require.NoError(t, err)
	require.Equal(t, slack.ListChannelsBranchListed, result.Branch)
	require.Equal(t, []slack.Channel{
		{ChannelID: "C123", Name: "general", IsMember: true, Topic: "News", Purpose: "Company-wide"},
		{ChannelID: "G456", Name: "general-leads", IsPrivate: true},
	}, result.Value.Channels)
	require.Equal(t, "next", result.Value.NextCursor)
}

func TestListChannelsMemberOnlyReadsJoinedChannels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/users.conversations", request.URL.Path)
		require.Equal(t, "Bearer bot-token", request.Header.Get("Authorization"))
		require.Equal(t, "public_channel", request.URL.Query().Get("types"))
		require.Equal(t, "5", request.URL.Query().Get("limit"))
		require.Equal(t, "page-2", request.URL.Query().Get("cursor"))
		_, _ = response.Write([]byte(`{"ok":true,"channels":[{"id":"C123","name":"general"}]}`))
	}))
	defer server.Close()
	client := newSlackClient(t, server.URL)
	result, err := sdkgo.RunQuery(newSlackDexContext("list-member-channels"), client.ListChannels(), slackConnection, slack.ListChannelsInput{
		MemberOnly: true, Visibility: "public", Limit: 5, Cursor: "page-2",
	})
	require.NoError(t, err)
	require.Equal(t, slack.ListChannelsBranchListed, result.Branch)
	require.Equal(t, []slack.Channel{{ChannelID: "C123", Name: "general", IsMember: true}}, result.Value.Channels)
	require.Empty(t, result.Value.NextCursor)
}

func TestListChannelsRejectsInvalidInputBeforeRequest(t *testing.T) {
	client := newSlackClient(t, "http://127.0.0.1:1")
	for _, input := range []slack.ListChannelsInput{{Limit: -1}, {Limit: 1000}, {Visibility: "archived"}} {
		result, err := sdkgo.RunQuery(newSlackDexContext("list-channels-defect"), client.ListChannels(), slackConnection, input)
		require.NoError(t, err)
		require.Equal(t, slack.ListChannelsBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
}

func TestDiscoveryReadsRefreshRejectedBotTokenOnce(t *testing.T) {
	testCases := []struct {
		name   string
		path   string
		body   string
		invoke func(*slack.Client) (sdkgo.BranchID, error)
		branch sdkgo.BranchID
	}{
		{
			name: "listChannels", path: "/conversations.list", body: `{"ok":true,"channels":[{"id":"C123","name":"general"}]}`,
			invoke: func(client *slack.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newSlackDexContext("list-channels-refresh"), client.ListChannels(), slackConnection, slack.ListChannelsInput{})
				return result.Branch, err
			},
			branch: slack.ListChannelsBranchListed,
		},
		{
			name: "getWorkspaceIdentity", path: "/auth.test", body: `{"ok":true,"team":"Acme","team_id":"T123","user_id":"UBOT"}`,
			invoke: func(client *slack.Client) (sdkgo.BranchID, error) {
				result, err := sdkgo.RunQuery(newSlackDexContext("identity-refresh"), client.GetWorkspaceIdentity(), slackConnection, slack.GetWorkspaceIdentityInput{})
				return result.Branch, err
			},
			branch: slack.GetWorkspaceIdentityBranchRead,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				requests++
				require.Equal(t, testCase.path, request.URL.Path)
				if requests == 1 {
					require.Equal(t, "Bearer rejected-bot", request.Header.Get("Authorization"))
					_, _ = response.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
					return
				}
				require.Equal(t, "Bearer replacement-bot", request.Header.Get("Authorization"))
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
			require.NoError(t, err)
			branch, err := testCase.invoke(client)
			require.NoError(t, err)
			require.Equal(t, testCase.branch, branch)
			require.Equal(t, 2, requests)
			require.Equal(t, 1, provider.forcedRefreshes)
		})
	}
}
