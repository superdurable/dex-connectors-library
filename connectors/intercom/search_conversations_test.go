// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBuildConversationSearchQueryCombinesTypedFilters(t *testing.T) {
	query, err := intercom.BuildConversationSearchQuery(intercom.SearchConversationsInput{
		States:       []intercom.ConversationState{intercom.ConversationStateOpen, intercom.ConversationStateSnoozed},
		ContactEmail: "jane@acme.example.com", ContactIDs: []string{"5ba682d23d7cf92bef87bfd4"}, TagIDs: []string{"123456", "7890"},
		UpdatedSince: "2026-01-21T01:00:00+01:00",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"operator":"AND","value":[
		{"operator":"OR","value":[{"field":"state","operator":"=","value":"open"},{"field":"state","operator":"=","value":"snoozed"}]},
		{"field":"source.author.email","operator":"=","value":"jane@acme.example.com"},
		{"field":"contact_ids","operator":"=","value":"5ba682d23d7cf92bef87bfd4"},
		{"operator":"OR","value":[{"field":"tag_ids","operator":"=","value":"123456"},{"field":"tag_ids","operator":"=","value":"7890"}]},
		{"field":"updated_at","operator":">","value":1768953600}
	]}`, string(query))

	single, err := intercom.BuildConversationSearchQuery(intercom.SearchConversationsInput{States: []intercom.ConversationState{intercom.ConversationStateClosed}})
	require.NoError(t, err)
	require.JSONEq(t, `{"field":"state","operator":"=","value":"closed"}`, string(single), "one filter needs no AND group")
}

func TestBuildConversationSearchQueryRejectsUnsafeOrUnboundedFilters(t *testing.T) {
	tooManyContacts := make([]string, intercom.MaxSearchFilterValues+1)
	for index := range tooManyContacts {
		tooManyContacts[index] = fmt.Sprintf("contact%02d", index)
	}
	for name, input := range map[string]intercom.SearchConversationsInput{
		"no filter":               {},
		"unknown state":           {States: []intercom.ConversationState{"pending"}},
		"Zendesk status":          {States: []intercom.ConversationState{"solved"}},
		"duplicate state":         {States: []intercom.ConversationState{"open", "open"}},
		"display address":         {ContactEmail: "Jane <jane@acme.example.com>"},
		"contact ID with a quote": {ContactIDs: []string{`5ba6"},{"field":"state`}},
		"too many contacts":       {ContactIDs: tooManyContacts},
		"tag name instead of ID":  {TagIDs: []string{"billing"}},
		"date without offset":     {UpdatedSince: "2026-01-21T00:00:00"},
		"date only":               {UpdatedSince: "2026-01-21"},
	} {
		_, err := intercom.BuildConversationSearchQuery(input)
		require.Error(t, err, name)
	}
}

func TestSearchConversationsReadsOneBoundedPageWithACursor(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"conversation.list","total_count":41,
			"conversations":[`+strings.Replace(conversationJSON(t, testConversation, "open", nil), `"conversation_parts"`, `"ignored_parts"`, 1)+`],
			"pages":{"type":"pages","page":1,"per_page":2,"total_pages":21,"next":{"per_page":2,"starting_after":"WzE3MTk0OTM3NTcuMCwiMjU5NDgxOSJd"}}}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("search"), newIntercomClient(t, provider.URL).SearchConversations(), intercomConnection,
		intercom.SearchConversationsInput{States: []intercom.ConversationState{intercom.ConversationStateOpen}, PageSize: 2, Cursor: "WzE3MTk0OTM3NTcuMCwiMjU5NDgxNSJd"})
	require.NoError(t, err)
	require.Equal(t, intercom.SearchConversationsBranchSearched, result.Branch)
	require.Equal(t, 41, result.Value.TotalCount)
	require.Equal(t, "WzE3MTk0OTM3NTcuMCwiMjU5NDgxOSJd", result.Value.NextCursor)
	require.Len(t, result.Value.Conversations, 1)
	conversation := result.Value.Conversations[0]
	require.Equal(t, testConversation, conversation.ID)
	require.Equal(t, intercom.ConversationStateOpen, conversation.State)
	require.Empty(t, conversation.Source.Body, "search pages omit the first message body")
	require.Equal(t, "jane@acme.example.com", conversation.Source.Author.Email)
	require.Equal(t, []intercom.ConversationTag{{ID: "123456", Name: "billing"}}, conversation.Tags)
	require.Equal(t, time.Unix(1767225600, 0).UTC(), conversation.CreatedAt)

	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/conversations/search", request.path)
	require.JSONEq(t, `{"query":{"field":"state","operator":"=","value":"open"},"pagination":{"per_page":2,"starting_after":"WzE3MTk0OTM3NTcuMCwiMjU5NDgxNSJd"}}`, request.body)
}

func TestSearchConversationsDefaultsThePageSizeAndRejectsInvalidPages(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"conversation.list","total_count":0,"conversations":[],"pages":{"type":"pages","page":1}}`)
	})
	client := newIntercomClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newTestDexContext("search-default"), client.SearchConversations(), intercomConnection,
		intercom.SearchConversationsInput{UpdatedSince: "2026-01-21T00:00:00Z"})
	require.NoError(t, err)
	require.Equal(t, intercom.SearchConversationsBranchSearched, result.Branch)
	require.Empty(t, result.Value.Conversations)
	require.Empty(t, result.Value.NextCursor)
	require.Contains(t, provider.request(0).body, `"per_page":20`)

	for name, input := range map[string]intercom.SearchConversationsInput{
		"page too large":    {UpdatedSince: "2026-01-21T00:00:00Z", PageSize: intercom.MaxSearchPageSize + 1},
		"negative page":     {UpdatedSince: "2026-01-21T00:00:00Z", PageSize: -1},
		"cursor with space": {UpdatedSince: "2026-01-21T00:00:00Z", Cursor: "a b"},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("search-"+name), client.SearchConversations(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.SearchConversationsBranchDefect, result.Branch, name)
	}
	require.Equal(t, 1, provider.requestCount(), "invalid input sends no request")
}

func TestSearchConversationsRejectsAnInvalidPage(t *testing.T) {
	for name, body := range map[string]string{
		"not a list":      `{"type":"admin.list","admins":[]}`,
		"no array":        `{"type":"conversation.list","total_count":0}`,
		"larger than ask": `{"type":"conversation.list","conversations":[` + conversationJSON(t, "1", "open", nil) + `,` + conversationJSON(t, "2", "open", nil) + `]}`,
		"bad ID":          `{"type":"conversation.list","conversations":[{"type":"conversation","id":"abc"}]}`,
	} {
		provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newTestDexContext("search-invalid-"+name), newIntercomClient(t, provider.URL).SearchConversations(), intercomConnection,
			intercom.SearchConversationsInput{States: []intercom.ConversationState{"open"}, PageSize: 1})
		require.NoError(t, err)
		require.Equal(t, intercom.SearchConversationsBranchInvalidResponse, result.Branch, name)
	}
}
