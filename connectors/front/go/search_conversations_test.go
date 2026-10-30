// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchConversationsSendsTypedFiltersAsOneEncodedQuery(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"_pagination":{"next":"https://acme.api.frontapp.com/conversations/search/inbox%3Ainb_41w25?limit=2&page_token=2d018a5809eb90d3"},
			"_total":3,"_results":[`+conversationJSON(t, testConversation{id: testConversationID, status: "unassigned"})+`,`+
			conversationJSON(t, testConversation{id: "cnv_yo1kg5q", status: "archived", assigneeID: testTeammateID, tagIDs: []string{testTagID}, isSnoozed: true})+`]}`)
	})
	activityAfter := time.Unix(1649974320, 0)
	result, err := sdkgo.RunQuery(newTestDexContext("search"), newFrontClient(t, provider.URL).SearchConversations(), frontConnection,
		front.SearchConversationsInput{
			Text: `"double charge"`, InboxID: "inb_41w25", TagID: testTagID,
			Statuses:       []front.SearchStatusFilter{front.SearchStatusOpen, front.SearchStatusUnassigned, front.SearchStatusOpen},
			RecipientEmail: "jane+billing@acme.example.com", AssigneeID: testTeammateID, ActivityAfter: &activityAfter, PageSize: 2,
		})
	require.NoError(t, err)
	require.Equal(t, front.SearchConversationsBranchSearched, result.Branch)
	const query = `"double charge" inbox:inb_41w25 tag:tag_13o8r1 is:open is:unassigned recipient:jane+billing@acme.example.com assignee:tea_2thf after:1649974320`
	require.Equal(t, query, result.Value.Query)
	require.Equal(t, "/conversations/search/%22double%20charge%22%20inbox%3Ainb_41w25%20tag%3Atag_13o8r1%20is%3Aopen%20is%3Aunassigned%20"+
		"recipient%3Ajane%2Bbilling%40acme.example.com%20assignee%3Atea_2thf%20after%3A1649974320?limit=2", provider.request(0).path)
	require.Equal(t, "Bearer "+testAPIToken, provider.request(0).authorization)
	require.Equal(t, 3, result.Value.TotalCount)
	require.Equal(t, "2d018a5809eb90d3", result.Value.NextPageToken)
	require.Len(t, result.Value.Conversations, 2)
	first, second := result.Value.Conversations[0], result.Value.Conversations[1]
	require.Equal(t, front.ConversationStatusUnassigned, first.Status)
	require.Nil(t, first.Assignee)
	require.Equal(t, &front.Recipient{Handle: "jane@acme.example.com", Role: "from", Name: "Jane Smith", ContactID: "crd_1y8sp71"}, first.Recipient)
	require.Equal(t, time.UnixMilli(1767225600123).UTC(), first.CreatedAt)
	require.Equal(t, front.ConversationStatusArchived, second.Status)
	require.True(t, second.IsSnoozed, "Front reports a snoozed conversation as archived with a reminder")
	require.Equal(t, testTeammateID, second.Assignee.ID)
	require.Equal(t, []front.Tag{{ID: testTagID, Name: "name-of-" + testTagID}}, second.Tags)
}

func TestSearchConversationsSendsThePageTokenAndReportsTheLastPage(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"_pagination":{"next":null},"_total":0,"_results":[]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("search-next"), newFrontClient(t, provider.URL).SearchConversations(), frontConnection,
		front.SearchConversationsInput{Statuses: []front.SearchStatusFilter{front.SearchStatusOpen}, PageToken: "2d018a5809eb90d3"})
	require.NoError(t, err)
	require.Equal(t, front.SearchConversationsBranchSearched, result.Branch)
	require.Equal(t, "/conversations/search/is%3Aopen?limit=25&page_token=2d018a5809eb90d3", provider.request(0).path)
	require.Empty(t, result.Value.Conversations)
	require.NotNil(t, result.Value.Conversations)
	require.Empty(t, result.Value.NextPageToken)
}

func TestSearchConversationsRejectsANextLinkOutsideFrontsHosts(t *testing.T) {
	for name, next := range map[string]string{
		"foreign host":     "https://attacker.example.com/conversations/search/x?page_token=abc",
		"look-alike host":  "https://acme.api.frontapp.com.attacker.example/conversations/search/x?page_token=abc",
		"plain HTTP":       "http://api2.frontapp.com/conversations/search/x?page_token=abc",
		"user information": "https://user@api2.frontapp.com/conversations/search/x?page_token=abc",
		"other resource":   "https://api2.frontapp.com/contacts?page_token=abc",
		"no token":         "https://api2.frontapp.com/conversations/search/x?limit=2",
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, `{"_pagination":{"next":"`+next+`"},"_total":1,"_results":[]}`)
			})
			result, err := sdkgo.RunQuery(newTestDexContext("search-host"), newFrontClient(t, provider.URL).SearchConversations(), frontConnection,
				front.SearchConversationsInput{Text: "refund"})
			require.NoError(t, err)
			require.Equal(t, front.SearchConversationsBranchInvalidResponse, result.Branch)
			require.Empty(t, result.Value.NextPageToken)
			require.Empty(t, result.Value.Conversations)
		})
	}
}

func TestSearchConversationsValidatesFiltersBeforeAnyRequest(t *testing.T) {
	provider := newRecordingFront(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request may be sent") })
	client := newFrontClient(t, provider.URL)
	before := time.Unix(0, 0)
	for name, input := range map[string]front.SearchConversationsInput{
		"no criteria":          {},
		"blank text":           {Text: "   "},
		"control character":    {Text: "refund\ntag:tag_x"},
		"display name":         {RecipientEmail: "Jane <jane@acme.example.com>"},
		"filter injection":     {RecipientEmail: "jane@acme.example.com tag:tag_x"},
		"quoted local part":    {RecipientEmail: `"jane doe"@acme.example.com`},
		"inbox name":           {InboxID: "Support"},
		"tag name":             {TagID: "billing"},
		"assignee email":       {AssigneeID: "leela@planet-express.example.com"},
		"unknown status":       {Statuses: []front.SearchStatusFilter{"closed"}},
		"page size":            {Text: "refund", PageSize: front.MaxSearchPageSize + 1},
		"page token":           {Text: "refund", PageToken: "https://api2.frontapp.com/x"},
		"activity before 1970": {ActivityAfter: &before},
		"long text":            {Text: strings.Repeat("a", front.MaxSearchTextBytes+1)},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("search-"+name), client.SearchConversations(), frontConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, front.SearchConversationsBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}

func TestSearchConversationsRetriesARateLimitAfterRetryAfter(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "7")
		writeFrontError(t, response, http.StatusTooManyRequests)
	})
	_, err := sdkgo.RunQuery(newTestDexContext("search-429"), newFrontClient(t, provider.URL).SearchConversations(), frontConnection,
		front.SearchConversationsInput{Text: "refund"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	require.NotContains(t, err.Error(), providerSentinel)
}

func TestSearchConversationsMapsARejectionWithoutFrontsMessage(t *testing.T) {
	for status, kind := range map[int]sdkgo.FailureKind{
		http.StatusBadRequest: sdkgo.FailureValidation, http.StatusUnauthorized: sdkgo.FailureAuthentication,
		http.StatusForbidden: sdkgo.FailureAuthorization, http.StatusNotFound: sdkgo.FailureNotFound,
	} {
		provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) { writeFrontError(t, response, status) })
		result, err := sdkgo.RunQuery(newTestDexContext("search-rejected"), newFrontClient(t, provider.URL).SearchConversations(), frontConnection,
			front.SearchConversationsInput{Text: "refund"})
		require.NoError(t, err)
		require.Equal(t, front.SearchConversationsBranchProviderRejected, result.Branch, status)
		require.Equal(t, kind, result.Failure.Kind, status)
		require.NotContains(t, result.Failure.Message, providerSentinel)
		require.Equal(t, 1, provider.requestCount(), "a conclusive rejection is not repeated")
	}
}
