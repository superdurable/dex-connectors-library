// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func conversationPageJSON(hasNext bool, conversations ...string) string {
	next := ""
	if hasNext {
		next = `,"next":{"href":"https://api.helpscout.net/v2/conversations?page=3"}`
	}
	return fmt.Sprintf(`{"_embedded":{"conversations":[%s]},"_links":{"self":{"href":"x"},"first":{"href":"x"}%s},"page":{"number":2,"size":25,"totalElements":51,"totalPages":3}}`,
		strings.Join(conversations, ","), next)
}

func searchConversations(t *testing.T, client *helpscout.Client, input helpscout.SearchConversationsInput) (helpscout.SearchConversationsResult, error) {
	t.Helper()
	return sdkgo.RunQuery(newStepContext("search"), client.SearchConversations(), testConnection, input)
}

func TestSearchConversationsSendsTypedFiltersAndReturnsTheNextPage(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations": respondJSON(http.StatusOK, conversationPageJSON(true, conversationJSON(501, "pending", 123, "billing", "VIP"))),
	})
	result, err := searchConversations(t, newFakeBackedClient(t, fake), helpscout.SearchConversationsInput{
		MailboxID: 123, Status: helpscout.ConversationStatusPending, Tag: "billing", CustomerEmail: "jane+orders@acme.example.com",
		ModifiedSince: "2026-09-29T17:30:00.750-07:00", Page: 2,
	})
	require.NoError(t, err)
	require.Equal(t, helpscout.SearchConversationsBranchSearched, result.Branch)
	requests := fake.requestsTo(http.MethodGet, "/v2/conversations")
	require.Len(t, requests, 1)
	require.Equal(t, "Bearer "+sentinelToken, requests[0].authorization)
	require.Equal(t, url.Values{
		"mailbox": {"123"}, "status": {"pending"}, "tag": {"billing"}, "query": {`(email:"jane+orders@acme.example.com")`},
		"modifiedSince": {"2026-09-30T00:30:00Z"}, "page": {"2"},
	}, requests[0].query, "times are sent in UTC whole seconds, and the plus sign survives encoding")

	page := result.Value
	require.Equal(t, 2, page.Page)
	require.Equal(t, 3, page.NextPage)
	require.Equal(t, 3, page.TotalPages)
	require.EqualValues(t, 51, page.TotalConversations)
	require.Len(t, page.Conversations, 1)
	conversation := page.Conversations[0]
	require.EqualValues(t, 501, conversation.ID)
	require.Equal(t, helpscout.ConversationStatusPending, conversation.Status)
	require.EqualValues(t, 123, conversation.MailboxID)
	require.Equal(t, []string{"billing", "VIP"}, conversation.Tags)
	require.EqualValues(t, 99, conversation.Assignee.ID)
	require.Equal(t, "user", conversation.Assignee.Type)
	require.EqualValues(t, 238604, conversation.PrimaryCustomer.ID)
	require.Equal(t, "jane@acme.example.com", conversation.PrimaryCustomer.Email)
	require.Equal(t, time.Date(2026, time.September, 29, 22, 46, 22, 0, time.UTC), conversation.CreatedAt)
	require.Equal(t, time.Date(2026, time.September, 30, 7, 59, 0, 0, time.UTC), conversation.CustomerWaitingSince)
	require.True(t, conversation.ClosedAt.IsZero())
	require.Equal(t, "https://secure.helpscout.net/conversation/501/12", conversation.WebURL)
	require.Equal(t, "helpscout", result.Receipt.Provider)
	requireSecretFree(t, result)
}

func TestSearchConversationsWithoutFiltersListsActiveConversationsAndStopsOnTheLastPage(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations": respondJSON(http.StatusOK, `{"_links":{"self":{"href":"x"}},"page":{"number":1,"size":25,"totalElements":0,"totalPages":0}}`),
	})
	result, err := searchConversations(t, newFakeBackedClient(t, fake), helpscout.SearchConversationsInput{})
	require.NoError(t, err)
	require.Equal(t, helpscout.SearchConversationsBranchSearched, result.Branch)
	require.Equal(t, url.Values{"page": {"1"}}, fake.recordedRequests()[0].query, "Help Scout's default lists active conversations")
	require.Empty(t, result.Value.Conversations)
	require.NotNil(t, result.Value.Conversations)
	require.Zero(t, result.Value.NextPage)

	parameters, err := helpscout.BuildConversationSearchParameters(helpscout.SearchConversationsInput{Status: helpscout.ConversationStatusAll})
	require.NoError(t, err)
	require.Equal(t, "all", parameters.Get("status"))
}

func TestSearchConversationsRejectsInvalidFiltersWithoutARequest(t *testing.T) {
	fake := newFakeHelpScout(t, nil)
	client := newFakeBackedClient(t, fake)
	for _, input := range []helpscout.SearchConversationsInput{
		{Status: "open"},
		{Status: "solved"},
		{MailboxID: -1},
		{CustomerEmail: `jane@acme.example.com") OR (email:"eve@example.com`},
		{CustomerEmail: "Jane <jane@acme.example.com>"},
		{ModifiedSince: "2026-09-29T17:30:00"},
		{Tag: "billing,refund"},
		{Tag: " billing"},
		{Page: helpscout.MaxSearchPage + 1},
		{Page: -1},
	} {
		result, err := searchConversations(t, client, input)
		require.NoError(t, err, input)
		require.Equal(t, helpscout.SearchConversationsBranchDefect, result.Branch, input)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, input)
	}
	require.Empty(t, fake.recordedRequests())
}

func TestSearchConversationsClassifiesHelpScoutErrorsWithoutTheirText(t *testing.T) {
	for _, test := range []struct {
		name          string
		handler       http.HandlerFunc
		branch        sdkgo.BranchID
		kind          sdkgo.FailureKind
		isRetry       bool
		retryAfter    time.Duration
		messageSubstr string
	}{
		{name: "rate limit", handler: func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("X-RateLimit-Retry-After", "17")
			writeJSON(response, http.StatusTooManyRequests, helpScoutErrorBody("Too many requests", nil))
		}, isRetry: true, kind: sdkgo.FailureRateLimit, retryAfter: 17 * time.Second},
		{name: "gateway timeout", handler: respondJSON(http.StatusGatewayTimeout, helpScoutErrorBody("Internal Timeout", nil)), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "bad request", handler: respondJSON(http.StatusBadRequest, helpScoutErrorBody("Bad request", map[string]string{"status": "EnumValue"})),
			branch: helpscout.SearchConversationsBranchProviderRejected, kind: sdkgo.FailureValidation, messageSubstr: "[status=EnumValue]"},
		{name: "unauthorized", handler: respondJSON(http.StatusUnauthorized, helpScoutErrorBody("Unauthorized", nil)),
			branch: helpscout.SearchConversationsBranchProviderRejected, kind: sdkgo.FailureAuthentication, messageSubstr: "active Help Scout user"},
		{name: "api not enabled", handler: respondJSON(http.StatusForbidden, helpScoutErrorBody("API not enabled", nil)),
			branch: helpscout.SearchConversationsBranchProviderRejected, kind: sdkgo.FailureAuthorization, messageSubstr: "API is not enabled"},
		{name: "redirect", handler: respondStatus(http.StatusFound, map[string]string{"Location": "https://elsewhere.example/"}),
			branch: helpscout.SearchConversationsBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "oversized page", handler: respondJSON(http.StatusOK, conversationPageJSON(false, conversationJSON(1, "active", 1, strings.Repeat("x", 64)))),
			branch: helpscout.SearchConversationsBranchInvalidResponse, kind: sdkgo.FailureResponseTooLarge},
		{name: "credential reflection", handler: respondJSON(http.StatusOK, `{"echo":"`+sentinelToken+`"}`),
			branch: helpscout.SearchConversationsBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "malformed conversation", handler: respondJSON(http.StatusOK, conversationPageJSON(false, `{"id":0}`)),
			branch: helpscout.SearchConversationsBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeHelpScout(t, map[string]http.HandlerFunc{"GET /v2/conversations": test.handler})
			config := helpscout.Config{}
			if test.name == "oversized page" {
				config.MaxResponseBytes = 256
			}
			client := newTestClient(t, fake.redirectingClient(), staticCredentials(sentinelWebhookSecret), config)
			result, err := searchConversations(t, client, helpscout.SearchConversationsInput{Status: helpscout.ConversationStatusActive})
			if test.isRetry {
				retry := requireRetry(t, err, test.kind)
				require.Equal(t, test.retryAfter, retryDelay(err))
				require.NotContains(t, retry.Failure.Message, providerSecretMessage)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.messageSubstr)
			requireSecretFree(t, result)
			require.Len(t, fake.recordedRequests(), 1, "a redirect is never followed")
		})
	}
}
