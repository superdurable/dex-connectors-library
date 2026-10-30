// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSearchConversationsSendsReamazeListFiltersAndDecodesAPage(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{
			"page_size": 30, "page_count": 3, "total_count": 61,
			"conversations": []any{conversationJSON("double-charge", 1, []string{"billing", "vip"}), conversationJSON("refund-status", 2, nil)},
		})
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("search"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection,
		reamaze.SearchConversationsInput{
			Filter: reamaze.ConversationFilterAll, RequesterEmail: "jane@acme.example.com", Tags: []string{"billing", "vip"},
			Channel: "support", Sort: reamaze.ConversationSortChanged, CustomerMessageSince: "2026-01-21T00:00:00Z",
			CustomerMessageUntil: "2026-01-28T00:00:00Z", Page: 2,
		})
	require.NoError(t, err)
	require.Equal(t, reamaze.SearchConversationsBranchSearched, result.Branch)
	require.Equal(t, map[string][]string{
		"filter": {"all"}, "for": {"jane@acme.example.com"}, "tag": {"billing,vip"}, "category": {"support"}, "sort": {"changed"},
		"start_date": {"2026-01-21T00:00:00Z"}, "end_date": {"2026-01-28T00:00:00Z"}, "page": {"2"},
	}, provider.request(0).query)
	output := result.Value
	require.Equal(t, 61, output.TotalCount)
	require.Equal(t, 3, output.PageCount)
	require.Equal(t, 3, output.NextPage)
	require.Len(t, output.Conversations, 2)
	conversation := output.Conversations[0]
	require.Equal(t, "double-charge", conversation.ID)
	require.Equal(t, reamaze.ConversationStatusResponded, conversation.Status)
	require.Equal(t, []string{"billing", "vip"}, conversation.Tags)
	require.Equal(t, reamaze.ConversationChannel{Slug: "support", Name: "Support", Type: 1}, conversation.Channel)
	require.Equal(t, &reamaze.ConversationParticipant{Name: "Jane Smith", Email: "jane@acme.example.com"}, conversation.Requester)
	require.Nil(t, conversation.Assignee)
	require.Empty(t, conversation.FirstMessage, "search pages stay small")
	require.Equal(t, time.Date(2026, 1, 26, 22, 2, 0, 123000000, time.UTC), conversation.CreatedAt)
	require.Equal(t, time.Date(2026, 1, 27, 10, 0, 0, 0, time.UTC), *conversation.LastCustomerMessageAt)
	require.Equal(t, reamaze.ConversationStatusDone, output.Conversations[1].Status)
}

func TestSearchConversationsDefaultsToReamazesUnarchivedViewAndFirstPage(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 1))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("search-default"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection,
		reamaze.SearchConversationsInput{})
	require.NoError(t, err)
	require.Equal(t, map[string][]string{"page": {"1"}}, provider.request(0).query)
	require.Empty(t, result.Value.Conversations)
	require.NotNil(t, result.Value.Conversations)
	require.Zero(t, result.Value.NextPage)
}

func TestSearchConversationsRejectsInvalidFiltersWithoutARequest(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 1))
	})
	for name, input := range map[string]reamaze.SearchConversationsInput{
		"unknown filter":   {Filter: "closed"},
		"display address":  {RequesterEmail: "Jane <jane@acme.example.com>"},
		"comma tag":        {Tags: []string{"billing,vip"}},
		"duplicate tag":    {Tags: []string{"vip", "VIP"}},
		"channel path":     {Channel: "support/../x"},
		"unknown sort":     {Sort: "oldest"},
		"date only":        {CustomerMessageSince: "2026-01-21"},
		"negative page":    {Page: -1},
		"page beyond many": {Page: reamaze.MaxSearchPage + 1},
	} {
		result, err := sdkgo.RunQuery(newReamazeDexContext("invalid-search"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, reamaze.SearchConversationsBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}

func TestSearchConversationsRejectsMalformedPages(t *testing.T) {
	for name, body := range map[string]string{
		"not an object":    `[]`,
		"no list":          `{"page_count":1,"total_count":0}`,
		"no counters":      `{"conversations":[]}`,
		"missing slug":     `{"page_count":1,"total_count":1,"conversations":[{"status":0,"created_at":"2026-01-26T14:02:00Z"}]}`,
		"missing status":   `{"page_count":1,"total_count":1,"conversations":[{"slug":"a","created_at":"2026-01-26T14:02:00Z"}]}`,
		"bad created time": `{"page_count":1,"total_count":1,"conversations":[{"slug":"a","status":0,"created_at":"yesterday"}]}`,
		"page overflow":    `{"page_size":1,"page_count":1,"total_count":2,"conversations":[{"slug":"a","status":0,"created_at":"2026-01-26T14:02:00Z"},{"slug":"b","status":0,"created_at":"2026-01-26T14:02:00Z"}]}`,
	} {
		provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newReamazeDexContext("malformed"), newReamazeClient(t, provider.URL).SearchConversations(), reamazeConnection,
			reamaze.SearchConversationsInput{})
		require.NoError(t, err, name)
		require.Equal(t, reamaze.SearchConversationsBranchInvalidResponse, result.Branch, name)
		require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind, name)
	}
}
