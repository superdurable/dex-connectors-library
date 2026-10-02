// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var searchBase = time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)

func TestSearchMessagesFiltersNewestFirstWithReceivedTimeLeading(t *testing.T) {
	fake := newGraphFake(t)
	older := seedCustomerMessage(fake, "Refund request", searchBase)
	latest := seedCustomerMessage(fake, "Refund request for order 88213", searchBase.Add(time.Hour))
	fake.AddMessage(graphtest.SeedMessage{FromAddress: "jane@acme.example.com.au", Subject: "Refund request", ReceivedAt: searchBase.Add(2 * time.Hour)})
	fake.AddMessage(graphtest.SeedMessage{FromAddress: testCustomer, Subject: "Refund", IsRead: true, ReceivedAt: searchBase.Add(3 * time.Hour)})
	fake.AddMessage(graphtest.SeedMessage{FromAddress: testCustomer, Subject: "Refund request", Folder: "archive", ReceivedAt: searchBase.Add(4 * time.Hour)})
	client, _ := delegatedClient(t, fake)

	after, before := searchBase.Add(-time.Minute), searchBase.Add(5*time.Hour)
	result, err := sdkgo.RunQuery(newOutlookDexContext("search"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{
		From: testCustomer, SubjectContains: "refund REQUEST", ReceivedAfter: &after, ReceivedBefore: &before, IsUnread: true,
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchSearched, result.Branch, result.Failure)
	require.Equal(t, "inbox", result.Value.Folder)
	require.Len(t, result.Value.Messages, 2, "the lookalike, the read message, and the archived message are excluded")
	require.Equal(t, []string{latest, older}, []string{result.Value.Messages[0].ID, result.Value.Messages[1].ID})
	require.False(t, result.Value.HasMore)
	summary := result.Value.Messages[0]
	require.Equal(t, &outlookmail.EmailAddress{Name: "Jane Smith", Address: testCustomer}, summary.From)
	require.Equal(t, searchBase.Add(time.Hour), summary.ReceivedAt)
	require.Equal(t, outlookmail.FlagStatusNotFlagged, summary.FlagStatus)
	require.Equal(t, fake.FolderID("inbox"), summary.FolderID)
	require.Equal(t, "I was charged twice for order 88213.", summary.Preview)

	request := fake.Requests(graphtest.EndpointListFolderMessages)[0]
	require.Equal(t, "/v1.0/me/mailFolders/inbox/messages", request.Path)
	require.Equal(t, "receivedDateTime ge 2026-09-28T08:59:00Z and receivedDateTime lt 2026-09-28T14:00:00Z and "+
		"from/emailAddress/address eq 'jane@acme.example.com' and isRead eq false and contains(subject,'refund REQUEST')", request.Query.Get("$filter"))
	require.Equal(t, "receivedDateTime desc", request.Query.Get("$orderby"))
	require.Equal(t, "10", request.Query.Get("$top"))
	require.Contains(t, request.Query.Get("$select"), "bodyPreview")
	require.NotContains(t, request.Query.Get("$select"), "body,", "summaries never download bodies")
}

func TestSearchMessagesPagesWithTheValidatedNextLink(t *testing.T) {
	fake := newGraphFake(t)
	for index := 0; index < 5; index++ {
		seedCustomerMessage(fake, "Question", searchBase.Add(time.Duration(index)*time.Minute))
	}
	client, _ := delegatedClient(t, fake)
	first, err := sdkgo.RunQuery(newOutlookDexContext("page-1"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{Limit: 3})
	require.NoError(t, err)
	require.Len(t, first.Value.Messages, 3)
	require.True(t, first.Value.HasMore)
	require.True(t, strings.HasPrefix(first.Value.NextPageCursor, "https://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?"))

	second, err := sdkgo.RunQuery(newOutlookDexContext("page-2"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{
		PageCursor: first.Value.NextPageCursor, Limit: 50, From: "ignored@example.com",
	})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchSearched, second.Branch, second.Failure)
	require.Len(t, second.Value.Messages, 2, "the cursor carries the first page's filters and size")
	require.False(t, second.Value.HasMore)
	require.True(t, second.Value.Messages[0].ReceivedAt.Before(first.Value.Messages[2].ReceivedAt))
}

func TestSearchMessagesRejectsACursorOutsideGraphMessages(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	for _, cursor := range []string{
		"https://attacker.example/v1.0/me/mailFolders/inbox/messages?$skip=10",
		"http://graph.microsoft.com/v1.0/me/mailFolders/inbox/messages?$skip=10",
		"https://graph.microsoft.com/v1.0/me/drive/root/children",
		"https://graph.microsoft.com/beta/me/messages",
	} {
		result, err := sdkgo.RunQuery(newOutlookDexContext("cursor"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{PageCursor: cursor})
		require.NoError(t, err)
		require.Equal(t, outlookmail.SearchMessagesBranchDefect, result.Branch, cursor)
	}
	require.Zero(t, fake.RequestCount(graphtest.EndpointListFolderMessages))
}

func TestSearchMessagesAcceptsFolderIDsAndWellKnownNamesOnly(t *testing.T) {
	fake := newGraphFake(t)
	support := fake.AddFolder("Support")
	fake.AddMessage(graphtest.SeedMessage{Folder: support, FromAddress: testCustomer, Subject: "In support", ReceivedAt: searchBase})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("folder-id"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{Folder: support})
	require.NoError(t, err)
	require.Len(t, result.Value.Messages, 1)
	result, err = sdkgo.RunQuery(newOutlookDexContext("well-known"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{Folder: "Archive"})
	require.NoError(t, err)
	require.Equal(t, "archive", result.Value.Folder)
	for _, input := range []outlookmail.SearchMessagesInput{
		{Folder: "Inbox/Support"}, {Folder: "inbox?$top=999"}, {Limit: 51}, {From: "Jane <jane@acme.example.com>"},
		{SubjectContains: "line\nbreak"}, {ReceivedAfter: &searchBase, ReceivedBefore: &searchBase},
	} {
		result, err := sdkgo.RunQuery(newOutlookDexContext("invalid"), client.SearchMessages(), outlookConnection, input)
		require.NoError(t, err)
		require.Equal(t, outlookmail.SearchMessagesBranchDefect, result.Branch, input)
	}
}

func TestSearchMessagesMissingFolderIsAProviderRejection(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("missing-folder"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{Folder: "AAMkFolder-404_gone="})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	requireNoSecretsOrServerText(t, result.Failure)
}

func TestSearchMessagesQuotesSingleQuotesInODataLiterals(t *testing.T) {
	fake := newGraphFake(t)
	fake.AddMessage(graphtest.SeedMessage{FromAddress: "o'brien@acme.example.com", Subject: "Customer's refund", ReceivedAt: searchBase})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("quotes"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{SubjectContains: "Customer's"})
	require.NoError(t, err)
	require.Len(t, result.Value.Messages, 1)
	require.Contains(t, fake.Requests(graphtest.EndpointListFolderMessages)[0].Query.Get("$filter"), "contains(subject,'Customer''s')")
}
