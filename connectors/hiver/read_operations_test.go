// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListInboxesReadsOnePageAndItsNextToken(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[
			{"id":"101","display_name":"Customer Support","channel_type":"email","email":"support@acme.example.com","inbox_type":"user",
			 "is_authorised":true,"source_user":{"id":"12323","email":"SENTINEL@acme.example.com"},"created_at":1709036168,"updated_at":1709036168},
			{"id":102,"display_name":"Billing","channel_type":"email","email":"billing@acme.example.com","is_authorised":false}
		],"pagination":{"next_page":"eyJjdXJzb3IiOiIxMDIifQ=="}}}`)
	})
	result, err := sdkgo.RunQuery(newHiverDexContext("list"), newHiverClient(t, provider.URL).ListInboxes(), hiverConnection,
		hiver.ListInboxesInput{PageToken: "eyJjdXJzb3IiOiIxMDAifQ==", PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, hiver.ListInboxesBranchListed, result.Branch)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/v1/inboxes", request.path)
	require.Equal(t, map[string][]string{"limit": {"100"}, "next_page": {"eyJjdXJzb3IiOiIxMDAifQ=="}}, request.query)
	require.Equal(t, hiver.ListInboxesOutput{Inboxes: []hiver.Inbox{
		{ID: "101", DisplayName: "Customer Support", Email: "support@acme.example.com", ChannelType: "email", IsAuthorized: true},
		{ID: "102", DisplayName: "Billing", Email: "billing@acme.example.com", ChannelType: "email"},
	}, NextPageToken: "eyJjdXJzb3IiOiIxMDIifQ=="}, result.Value)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "the source user's mailbox is not carried")
}

func TestListInputsAreValidatedBeforeAnyRequest(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	client := newHiverClient(t, provider.URL)
	for name, input := range map[string]hiver.ListConversationsInput{
		"missing inbox":      {},
		"inbox with a colon": {InboxID: "a:b"},
		"page size below 10": {InboxID: "101", PageSize: 5},
		"page size above 100": {
			InboxID: "101", PageSize: 101,
		},
		"page token with a space": {InboxID: "101", PageToken: "a b"},
	} {
		result, err := sdkgo.RunQuery(newHiverDexContext("list"), client.ListConversations(), hiverConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, hiver.ListConversationsBranchDefect, result.Branch, name)
	}
}

func TestListConversationsUsesTheDefaultPageSizeAndDecodesNumericIDs(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":{"results":[`+conversationJSON("538706170", "open", "1028399", []string{"56789"})+`,`+
			conversationJSON("538706171", "close", "", nil)+`],"pagination":{"next_page":null}}}`)
	})
	result, err := sdkgo.RunQuery(newHiverDexContext("list"), newHiverClient(t, provider.URL).ListConversations(), hiverConnection,
		hiver.ListConversationsInput{InboxID: "105902"})
	require.NoError(t, err)
	require.Equal(t, hiver.ListConversationsBranchListed, result.Branch)
	require.Equal(t, "/v1/inboxes/105902/conversations", provider.request(0).path)
	require.Equal(t, map[string][]string{"limit": {"50"}}, provider.request(0).query)
	require.Empty(t, result.Value.NextPageToken)
	require.Equal(t, []hiver.Conversation{
		{
			ID: "538706170", InboxID: "105902", Status: hiver.ConversationStatusOpen, Assignee: &hiver.ConversationAssignee{Type: "user", ID: "1028399"},
			TagIDs: []string{"56789"}, GmailThreadID: "19cfee91188070f8", PrivatePermalink: "https://v2.hiverhq.com/permalinks/pvt/201c00fa",
		},
		{
			ID: "538706171", InboxID: "105902", Status: hiver.ConversationStatusClosed, TagIDs: []string{},
			GmailThreadID: "19cfee91188070f8", PrivatePermalink: "https://v2.hiverhq.com/permalinks/pvt/201c00fa",
		},
	}, result.Value.Conversations, "Hiver's close is reported as closed")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL", "the public permalink is not carried")
}

func TestListConversationsBranches(t *testing.T) {
	for name, test := range map[string]struct {
		status     int
		body       string
		wantBranch sdkgo.BranchID
	}{
		"unknown inbox":      {status: http.StatusNotFound, body: `{"Message":"not found"}`, wantBranch: hiver.ListConversationsBranchNotFound},
		"missing results":    {status: http.StatusOK, body: `{"data":{"pagination":{"next_page":null}}}`, wantBranch: hiver.ListConversationsBranchInvalidResponse},
		"invalid next page":  {status: http.StatusOK, body: `{"data":{"results":[],"pagination":{"next_page":"a b"}}}`, wantBranch: hiver.ListConversationsBranchInvalidResponse},
		"conversation no id": {status: http.StatusOK, body: `{"data":{"results":[{"status":"open"}],"pagination":{}}}`, wantBranch: hiver.ListConversationsBranchInvalidResponse},
		"fractional id":      {status: http.StatusOK, body: `{"data":{"results":[{"id":1.5,"status":"open"}],"pagination":{}}}`, wantBranch: hiver.ListConversationsBranchInvalidResponse},
		"missing status":     {status: http.StatusOK, body: `{"data":{"results":[{"id":1}],"pagination":{}}}`, wantBranch: hiver.ListConversationsBranchInvalidResponse},
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, test.status, test.body)
		})
		result, err := sdkgo.RunQuery(newHiverDexContext("list"), newHiverClient(t, provider.URL).ListConversations(), hiverConnection,
			hiver.ListConversationsInput{InboxID: "105902"})
		require.NoError(t, err, name)
		require.Equal(t, test.wantBranch, result.Branch, name)
	}
}

func TestGetConversationAcceptsTheDocumentedArrayAndAnObject(t *testing.T) {
	for name, body := range map[string]string{
		"array":  `{"data":[` + conversationJSON(`"538706170"`, "pending", `"1028399"`, []string{"1234322", "343434"}) + `]}`,
		"object": `{"data":` + conversationJSON(`538706170`, "pending", `1028399`, []string{"1234322", "343434"}) + `}`,
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newHiverDexContext("read"), newHiverClient(t, provider.URL).GetConversation(), hiverConnection,
			hiver.GetConversationInput{InboxID: "105902", ConversationID: "19cfee91188070f8"})
		require.NoError(t, err, name)
		require.Equal(t, hiver.GetConversationBranchFound, result.Branch, name)
		require.Equal(t, "/v1/inboxes/105902/conversations/19cfee91188070f8", provider.request(0).path, "a Gmail thread ID is accepted")
		require.Equal(t, hiver.ConversationDetails{
			Conversation: hiver.Conversation{
				ID: "538706170", InboxID: "105902", Status: hiver.ConversationStatusPending,
				Assignee: &hiver.ConversationAssignee{Type: "user", ID: "1028399"}, TagIDs: []string{"1234322", "343434"},
				GmailThreadID: "19cfee91188070f8", PrivatePermalink: "https://v2.hiverhq.com/permalinks/pvt/201c00fa",
			},
			Messages: []hiver.ConversationMessage{{HiverMessageID: "834466048", GmailMessageID: "19cfee91188070f8"}},
		}, result.Value, name)
		require.Equal(t, "538706170", result.Receipt.ProviderObjectID)
	}
}

func TestGetConversationRejectsAmbiguousOrUnsafeData(t *testing.T) {
	for name, body := range map[string]string{
		"two conversations":   `{"data":[` + conversationJSON("1", "open", "", nil) + `,` + conversationJSON("2", "open", "", nil) + `]}`,
		"no data":             `{"conversation":{}}`,
		"script permalink":    `{"data":{"id":1,"status":"open","private_permalink":"javascript:alert(1)"}}`,
		"invalid message ID":  `{"data":{"id":1,"status":"open","message_ids":[{"gmail_message_id":"../x"}]}}`,
		"empty message":       `{"data":{"id":1,"status":"open","message_ids":[{}]}}`,
		"status with markup":  `{"data":{"id":1,"status":"<b>open</b>"}}`,
		"assignee with colon": `{"data":{"id":1,"status":"open","assignee":{"assignee_type":"user","assignee_id":"a:b"}}}`,
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
			writeJSON(t, response, http.StatusOK, body)
		})
		result, err := sdkgo.RunQuery(newHiverDexContext("read"), newHiverClient(t, provider.URL).GetConversation(), hiverConnection,
			hiver.GetConversationInput{InboxID: "105902", ConversationID: "1"})
		require.NoError(t, err, name)
		require.Equal(t, hiver.GetConversationBranchInvalidResponse, result.Branch, name)
	}
}
