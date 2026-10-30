// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	usersSearchPage = `{"data":{"results":[{"id":"456342","first_name":"Phoebe","last_name":"Buffay","email":"P.Buffay@acme.example.com",
		"phone_number":"SENTINEL","is_joined":true}],"pagination":{"next_page":null}}}`
	firstTagPage  = `{"data":{"results":[{"id":"56789","name":"Priority","color_code":"#ce93d8","type":"user","created_at":1708945347}],"pagination":{"next_page":"cGFnZTI="}}}`
	secondTagPage = `{"data":{"results":[{"id":784268,"name":"escalated"},{"id":"784269","name":"Needs Reply"}],"pagination":{"next_page":null}}}`
)

func TestUpdateConversationResolvesNamesThenPatchesAndReadsBack(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/users/search"):
			writeJSON(t, response, http.StatusOK, usersSearchPage)
		case strings.HasSuffix(request.URL.Path, "/tags") && request.URL.Query().Get("next_page") == "":
			writeJSON(t, response, http.StatusOK, firstTagPage)
		case strings.HasSuffix(request.URL.Path, "/tags"):
			writeJSON(t, response, http.StatusOK, secondTagPage)
		case request.Method == http.MethodPatch:
			response.WriteHeader(http.StatusNoContent)
		default:
			writeJSON(t, response, http.StatusOK, `{"data":[`+conversationJSON("573741352", "close", "456342", []string{"784268"})+`]}`)
		}
	})
	result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection, hiver.UpdateConversationInput{
		InboxID: "105902", ConversationID: "573741352", Status: hiver.ConversationStatusClosed, AssigneeEmail: "p.buffay@acme.example.com",
		ApplyTagNames: []string{"Escalated"}, RemoveTagNames: []string{"Needs Reply", "Priority"},
	})
	require.NoError(t, err)
	require.Equal(t, hiver.UpdateConversationBranchUpdated, result.Branch)
	require.Equal(t, 5, provider.requestCount())
	require.Equal(t, "/v1/inboxes/105902/users/search", provider.request(0).path)
	require.Equal(t, map[string][]string{"email": {"p.buffay@acme.example.com"}}, provider.request(0).query)
	require.Equal(t, map[string][]string{"limit": {"100"}}, provider.request(1).query)
	require.Equal(t, map[string][]string{"limit": {"100"}, "next_page": {"cGFnZTI="}}, provider.request(2).query)
	patch := provider.request(3)
	require.Equal(t, http.MethodPatch, patch.method)
	require.Equal(t, "/v1/inboxes/105902/conversations/573741352", patch.path)
	require.Equal(t, "application/json", patch.header.Get("Content-Type"))
	require.JSONEq(t, `{"status":{"name":"close"},"assignee":{"email":"p.buffay@acme.example.com"},
		"tags":{"to_apply":["784268"],"to_remove":["784269","56789"]}}`, patch.body)
	require.Equal(t, http.MethodGet, provider.request(4).method)
	require.Equal(t, hiver.UpdateConversationOutput{
		Conversation: hiver.Conversation{
			ID: "573741352", InboxID: "105902", Status: hiver.ConversationStatusClosed, Assignee: &hiver.ConversationAssignee{Type: "user", ID: "456342"},
			TagIDs: []string{"784268"}, GmailThreadID: "19cfee91188070f8", PrivatePermalink: "https://v2.hiverhq.com/permalinks/pvt/201c00fa",
		},
		AssigneeUserID: "456342", AppliedTags: []hiver.Tag{{ID: "784268", Name: "escalated"}},
		RemovedTags: []hiver.Tag{{ID: "784269", Name: "Needs Reply"}, {ID: "56789", Name: "Priority"}},
	}, result.Value, "Escalated matches escalated ignoring case")
}

func TestUpdateConversationStatusOnlySendsOnlyTheStatus(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPatch {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"data":[`+conversationJSON("573741352", "pending", "", nil)+`]}`)
	})
	result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection,
		hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "573741352", Status: hiver.ConversationStatusPending})
	require.NoError(t, err)
	require.Equal(t, hiver.UpdateConversationBranchUpdated, result.Branch)
	require.Equal(t, 2, provider.requestCount(), "no user or tag lookup")
	require.JSONEq(t, `{"status":{"name":"pending"}}`, provider.request(0).body)
}

func TestUpdateConversationReadBackWithoutTheChangeIsRetried(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPatch {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"data":[`+conversationJSON("573741352", "open", "", nil)+`]}`)
	})
	_, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection,
		hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "573741352", Status: hiver.ConversationStatusClosed})
	require.Error(t, err, "the absolute change is sent again on the next attempt")
	require.Contains(t, err.Error(), "does not show its status yet")
}

func TestUpdateConversationRejectsUnknownNamesBeforeAnyChange(t *testing.T) {
	for name, test := range map[string]struct {
		input        hiver.UpdateConversationInput
		wantRequests int
		wantMessage  string
	}{
		"assignee outside the inbox": {
			input:        hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "1", AssigneeEmail: "ben@meridian.example.com"},
			wantRequests: 1, wantMessage: "assigneeEmail matches 0 users",
		},
		"unknown tag": {
			input:        hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "1", ApplyTagNames: []string{"vip"}},
			wantRequests: 2, wantMessage: "applyTagNames[0] is not a tag of the shared inbox",
		},
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			switch {
			case strings.HasSuffix(request.URL.Path, "/users/search"):
				writeJSON(t, response, http.StatusOK, `{"data":{"results":[],"pagination":{"next_page":null}}}`)
			case request.URL.Query().Get("next_page") == "":
				writeJSON(t, response, http.StatusOK, firstTagPage)
			default:
				writeJSON(t, response, http.StatusOK, secondTagPage)
			}
		})
		result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection, test.input)
		require.NoError(t, err, name)
		require.Equal(t, hiver.UpdateConversationBranchProviderRejected, result.Branch, name)
		require.Contains(t, result.Failure.Message, test.wantMessage, name)
		require.NotContains(t, result.Failure.Message, "vip", "the tag name is not echoed")
		require.Equal(t, test.wantRequests, provider.requestCount(), name)
		for index := range provider.requestCount() {
			require.Equal(t, http.MethodGet, provider.request(index).method, "%s: nothing was changed", name)
		}
	}
}

func TestUpdateConversationValidatesInputBeforeAnyRequest(t *testing.T) {
	provider := newRecordingHiver(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request expected") })
	client := newHiverClient(t, provider.URL)
	valid := hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "1", Status: hiver.ConversationStatusOpen}
	for name, change := range map[string]func(*hiver.UpdateConversationInput){
		"no change":       func(input *hiver.UpdateConversationInput) { input.Status = "" },
		"unknown status":  func(input *hiver.UpdateConversationInput) { input.Status = "solved" },
		"display address": func(input *hiver.UpdateConversationInput) { input.AssigneeEmail = "Phoebe <p@acme.example.com>" },
		"blank tag":       func(input *hiver.UpdateConversationInput) { input.ApplyTagNames = []string{" "} },
		"tag in both lists": func(input *hiver.UpdateConversationInput) {
			input.ApplyTagNames, input.RemoveTagNames = []string{"VIP"}, []string{"vip"}
		},
		"missing inbox": func(input *hiver.UpdateConversationInput) { input.InboxID = "" },
	} {
		input := valid
		change(&input)
		result, err := sdkgo.RunMutation(newHiverDexContext("update"), client.UpdateConversation(), hiverConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, hiver.UpdateConversationBranchDefect, result.Branch, name)
	}
}

func TestUpdateConversationOutcomes(t *testing.T) {
	for name, test := range map[string]struct {
		reply      func(t *testing.T, response http.ResponseWriter, request *http.Request)
		wantBranch sdkgo.BranchID
		isRetry    bool
	}{
		"unknown conversation": {
			reply: func(t *testing.T, response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, http.StatusNotFound, `{}`)
			},
			wantBranch: hiver.UpdateConversationBranchNotFound,
		},
		"rejected change": {
			reply: func(t *testing.T, response http.ResponseWriter, _ *http.Request) {
				writeJSON(t, response, http.StatusBadRequest, `{"Message":"x"}`)
			},
			wantBranch: hiver.UpdateConversationBranchProviderRejected,
		},
		"lost response": {
			reply:   func(t *testing.T, response http.ResponseWriter, _ *http.Request) { dropConnection(t, response) },
			isRetry: true,
		},
		"invalid read-back": {
			reply: func(t *testing.T, response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodPatch {
					response.WriteHeader(http.StatusNoContent)
					return
				}
				writeJSON(t, response, http.StatusOK, `{"data":"SENTINEL"}`)
			},
			wantBranch: hiver.UpdateConversationBranchInvalidResponse,
		},
	} {
		provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) { test.reply(t, response, request) })
		result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection,
			hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "1", Status: hiver.ConversationStatusOpen})
		if test.isRetry {
			require.Error(t, err, name)
			continue
		}
		require.NoError(t, err, name)
		require.Equal(t, test.wantBranch, result.Branch, name)
		require.NotContains(t, result.Failure.Message, "SENTINEL", name)
	}
}

func TestRejectedReadBackKeepsTheStatusKind(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPatch {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(t, response, http.StatusUnauthorized, `{"Message":"SENTINEL"}`)
	})
	result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection,
		hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "573741352", Status: hiver.ConversationStatusOpen})
	require.NoError(t, err)
	require.Equal(t, hiver.UpdateConversationBranchInvalidResponse, result.Branch, "the change was applied, so it is not providerRejected")
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Equal(t, "Hiver accepted the change but rejected the read-back: Hiver rejected the request (HTTP 401)", result.Failure.Message)
	require.Equal(t, 2, provider.requestCount())
}

func TestTagLookupBeyondTheLastPageReadSaysSo(t *testing.T) {
	provider := newRecordingHiver(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, firstTagPage)
	})
	result, err := sdkgo.RunMutation(newHiverDexContext("update"), newHiverClient(t, provider.URL).UpdateConversation(), hiverConnection,
		hiver.UpdateConversationInput{InboxID: "105902", ConversationID: "573741352", ApplyTagNames: []string{"Claimed"}})
	require.NoError(t, err)
	require.Equal(t, hiver.UpdateConversationBranchProviderRejected, result.Branch)
	require.Equal(t, "applyTagNames[0] is not among the tags on the first 5 pages Hiver returned; the change was not sent", result.Failure.Message)
	require.Equal(t, 5, provider.requestCount(), "every page had a next page, so the lookup stopped at its limit")
	require.Equal(t, "573741352", result.Receipt.ProviderObjectID, "the Receipt names the last tag page's call")
	require.NotEmpty(t, result.Receipt.CallID)
}
