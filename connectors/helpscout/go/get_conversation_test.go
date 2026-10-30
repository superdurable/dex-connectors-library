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

func getConversation(t *testing.T, client *helpscout.Client, input helpscout.GetConversationInput) helpscout.GetConversationResult {
	t.Helper()
	result, err := sdkgo.RunQuery(newStepContext("get"), client.GetConversation(), testConnection, input)
	require.NoError(t, err)
	return result
}

func TestGetConversationReturnsTheNewestThreadsWithinTheLimit(t *testing.T) {
	longBody := strings.Repeat("é", helpscout.MaxThreadBodyBytes)
	threads := []string{
		threadJSON(903, "note", "Refund already issued as REF-771.", "2026-09-30T09:00:00Z"),
		threadJSON(902, "message", longBody, "2026-09-30T08:00:00Z"),
		threadJSON(901, "customer", "I was charged twice.", "2026-09-29T22:46:22Z"),
	}
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501":         respondJSON(http.StatusOK, conversationJSON(501, "active", 123, "billing")),
		"GET /v2/conversations/501/threads": respondJSON(http.StatusOK, threadPageJSON(threads, false)),
	})
	result := getConversation(t, newFakeBackedClient(t, fake), helpscout.GetConversationInput{ConversationID: 501, ThreadLimit: 2})
	require.Equal(t, helpscout.GetConversationBranchFound, result.Branch)
	details := result.Value
	require.EqualValues(t, 501, details.Conversation.ID)
	require.Equal(t, "Double charge on order 88213", details.Conversation.Subject)
	require.True(t, details.HasOlderThreads, "a third thread exists")
	require.Len(t, details.Threads, 2)
	require.EqualValues(t, 903, details.Threads[0].ID, "newest first")
	require.Equal(t, "note", details.Threads[0].Type)
	require.Equal(t, time.Date(2026, time.September, 30, 9, 0, 0, 0, time.UTC), details.Threads[0].CreatedAt)
	require.True(t, details.Threads[1].IsBodyTruncated)
	require.LessOrEqual(t, len(details.Threads[1].Body), helpscout.MaxThreadBodyBytes)
	require.True(t, strings.HasSuffix(details.Threads[1].Body, "é"), "cut on a UTF-8 boundary")
	require.EqualValues(t, 238604, details.Threads[1].CreatedBy.ID)
	require.Zero(t, details.MergedIntoConversationID)
	require.Equal(t, url.Values{"page": {"1"}}, fake.requestsTo(http.MethodGet, "/v2/conversations/501/threads")[0].query)
	require.Equal(t, "501", result.Receipt.ProviderObjectID)
	requireSecretFree(t, result)

	defaultLimit := getConversation(t, newFakeBackedClient(t, fake), helpscout.GetConversationInput{ConversationID: 501})
	require.Len(t, defaultLimit.Value.Threads, 3)
	require.False(t, defaultLimit.Value.HasOlderThreads)
}

func TestGetConversationReportsAMergedConversationWithItsNewID(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/501": respondStatus(http.StatusMovedPermanently, map[string]string{"Location": "https://api.helpscout.net/v2/conversations/777"}),
		"GET /v2/conversations/502": respondStatus(http.StatusMovedPermanently, map[string]string{"Location": "https://evil.example/v2/conversations/777"}),
	})
	client := newFakeBackedClient(t, fake)
	merged := getConversation(t, client, helpscout.GetConversationInput{ConversationID: 501})
	require.Equal(t, helpscout.GetConversationBranchMerged, merged.Branch)
	require.EqualValues(t, 777, merged.Value.MergedIntoConversationID)
	require.Contains(t, merged.Failure.Message, "merged into conversation 777")
	require.Empty(t, fake.requestsTo(http.MethodGet, "/v2/conversations/777"), "the redirect is never followed")

	foreign := getConversation(t, client, helpscout.GetConversationInput{ConversationID: 502})
	require.Equal(t, helpscout.GetConversationBranchProviderRejected, foreign.Branch)
	require.Equal(t, sdkgo.FailureProtocol, foreign.Failure.Kind)
}

func TestGetConversationSelectsNotFoundInvalidResponseAndDefect(t *testing.T) {
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v2/conversations/404": respondJSON(http.StatusNotFound, helpScoutErrorBody("Not Found", nil)),
		"GET /v2/conversations/405": respondJSON(http.StatusOK, conversationJSON(405, "active", 1)),
		"GET /v2/conversations/406": respondJSON(http.StatusOK, conversationJSON(999, "active", 1)),
		"GET /v2/conversations/407": respondJSON(http.StatusOK, conversationJSON(407, "active", 1)),
		"GET /v2/conversations/407/threads": respondJSON(http.StatusOK,
			threadPageJSON([]string{`{"id":1,"type":"customer","createdAt":"not a time"}`}, false)),
	})
	client := newFakeBackedClient(t, fake)
	for _, test := range []struct {
		conversationID int64
		branch         sdkgo.BranchID
	}{
		{404, helpscout.GetConversationBranchNotFound},
		{405, helpscout.GetConversationBranchNotFound},
		{406, helpscout.GetConversationBranchInvalidResponse},
		{407, helpscout.GetConversationBranchInvalidResponse},
	} {
		result := getConversation(t, client, helpscout.GetConversationInput{ConversationID: test.conversationID})
		require.Equal(t, test.branch, result.Branch, test.conversationID)
		requireSecretFree(t, result)
	}
	before := len(fake.recordedRequests())
	for _, input := range []helpscout.GetConversationInput{{}, {ConversationID: 1, ThreadLimit: helpscout.MaxThreadLimit + 1}, {ConversationID: 1, ThreadLimit: -1}} {
		require.Equal(t, helpscout.GetConversationBranchDefect, getConversation(t, client, input).Branch, input)
	}
	require.Len(t, fake.recordedRequests(), before, "invalid input sends nothing")
}

func TestFindCustomerByEmailReturnsEveryMatchingProfile(t *testing.T) {
	customer := func(customerID int64, email string) string {
		return fmt.Sprintf(`{"id":%d,"firstName":"Jane","lastName":"Smith","organizationId":35,"conversationCount":4,
  "createdAt":"2026-01-10T12:34:12Z","updatedAt":"2026-01-11T20:18:33Z","background":"VIP",
  "_embedded":{"emails":[{"id":1,"value":%q,"type":"work"}],"phones":[{"id":2,"value":"555-0100","type":"work"}]}}`, customerID, email)
	}
	fake := newFakeHelpScout(t, map[string]http.HandlerFunc{
		"GET /v3/customers": func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Query().Get("email") {
			case "jane@acme.example.com":
				writeJSON(response, http.StatusOK, fmt.Sprintf(`{"_embedded":{"customers":[%s,%s]},"_links":{"self":{"href":"x"},"first":{"href":"x"},"next":{"href":"y"}}}`,
					customer(1001, "jane@acme.example.com"), customer(1002, "Jane@Acme.example.com")))
			default:
				writeJSON(response, http.StatusOK, `{"_embedded":{"customers":[]},"_links":{"self":{"href":"x"}}}`)
			}
		},
	})
	client := newFakeBackedClient(t, fake)
	found, err := sdkgo.RunQuery(newStepContext("find"), client.FindCustomerByEmail(), testConnection, helpscout.FindCustomerByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, helpscout.FindCustomerByEmailBranchFound, found.Branch)
	require.Len(t, found.Value.Customers, 2, "duplicate profiles are reported, not merged")
	require.True(t, found.Value.HasMore)
	require.EqualValues(t, 1001, found.Value.Customers[0].ID)
	require.Equal(t, []string{"jane@acme.example.com"}, found.Value.Customers[0].Emails)
	require.EqualValues(t, 35, found.Value.Customers[0].OrganizationID)
	require.Equal(t, time.Date(2026, time.January, 10, 12, 34, 12, 0, time.UTC), found.Value.Customers[0].CreatedAt)
	require.Equal(t, url.Values{"email": {"jane@acme.example.com"}}, fake.requestsTo(http.MethodGet, "/v3/customers")[0].query)

	missing, err := sdkgo.RunQuery(newStepContext("find"), client.FindCustomerByEmail(), testConnection, helpscout.FindCustomerByEmailInput{Email: "nobody@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, helpscout.FindCustomerByEmailBranchNotFound, missing.Branch)
	require.NotNil(t, missing.Value.Customers)
	require.Empty(t, missing.Value.Customers)

	for _, email := range []string{"", "Jane <jane@acme.example.com>", `jane"@acme.example.com`, "not-an-email"} {
		invalid, err := sdkgo.RunQuery(newStepContext("find"), client.FindCustomerByEmail(), testConnection, helpscout.FindCustomerByEmailInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, helpscout.FindCustomerByEmailBranchDefect, invalid.Branch, email)
	}
	require.Len(t, fake.recordedRequests(), 2)
}
