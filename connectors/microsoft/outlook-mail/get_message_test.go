// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetMessageReturnsPlainTextAndAttachmentsWithoutContent(t *testing.T) {
	fake := newGraphFake(t)
	id := fake.AddMessage(graphtest.SeedMessage{
		FromName: "Jane Smith", FromAddress: testCustomer, To: []string{testMailbox}, Cc: []string{"finance@acme.example.com"},
		ReplyTo: []string{"jane.billing@acme.example.com"}, Subject: "Invoice", Body: "<p>See the <b>invoice</b>.</p>", IsHTMLBody: true,
		ReceivedAt: searchBase, Categories: []string{"Billing"},
		Attachments: []graphtest.SeedAttachment{
			{Name: "invoice.pdf", ContentType: "application/pdf", Size: 48213},
			{Name: "logo.png", ContentType: "image/png", Size: 812, IsInline: true},
			{Name: "Original message", Size: 4100, Kind: "item"},
		},
	})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("get"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchFound, result.Branch, result.Failure)
	message := result.Value
	require.Equal(t, "text", message.TextSource)
	require.Equal(t, "See the \ninvoice\n.", message.Text, "Graph converted the HTML body to text")
	require.Equal(t, []outlookmail.EmailAddress{{Address: "jane.billing@acme.example.com"}}, message.ReplyTo)
	require.Equal(t, []string{"Billing"}, message.Categories)
	require.Equal(t, id, result.Receipt.ProviderObjectID)
	require.NotEmpty(t, message.WebLink)
	require.Equal(t, []outlookmail.Attachment{
		{ID: message.Attachments[0].ID, Name: "invoice.pdf", ContentType: "application/pdf", SizeBytes: 48213, Kind: outlookmail.AttachmentKindFile},
		{ID: message.Attachments[1].ID, Name: "logo.png", ContentType: "image/png", SizeBytes: 812, IsInline: true, Kind: outlookmail.AttachmentKindFile},
		{ID: message.Attachments[2].ID, Name: "Original message", SizeBytes: 4100, Kind: outlookmail.AttachmentKindItem},
	}, message.Attachments)

	read := fake.Requests(graphtest.EndpointGetMessage)[0]
	require.Contains(t, read.Header.Values("Prefer"), `outlook.body-content-type="text"`)
	require.Contains(t, read.Header.Values("Prefer"), `IdType="ImmutableId"`)
	listed := fake.Requests(graphtest.EndpointListAttachments)[0]
	require.Equal(t, "id,name,contentType,size,isInline", listed.Query.Get("$select"), "attachment content is never downloaded")
	stored, _ := fake.Message(id)
	require.False(t, stored.IsRead, "reading never marks the message read")
}

func TestGetMessageRemovesMarkupWhenGraphIgnoresTheTextPreference(t *testing.T) {
	fake := newGraphFake(t)
	fake.IgnoreTextBodyPreference()
	id := fake.AddMessage(graphtest.SeedMessage{
		FromAddress: testCustomer, Subject: "HTML", IsHTMLBody: true, ReceivedAt: searchBase,
		Body: "<html><head><style>p{color:red}</style></head><body><p>Hello</p><script>alert(1)</script><p>World</p></body></html>",
	})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("html"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
	require.NoError(t, err)
	require.Equal(t, "html", result.Value.TextSource)
	require.Equal(t, "Hello\nWorld", result.Value.Text)
	require.Zero(t, fake.RequestCount(graphtest.EndpointListAttachments), "a message without attachments needs one request")
}

func TestGetMessageCutsLongTextOnARuneBoundary(t *testing.T) {
	fake := newGraphFake(t)
	id := fake.AddMessage(graphtest.SeedMessage{FromAddress: testCustomer, Subject: "Long", Body: strings.Repeat("é", outlookmail.MaxTextBytes), ReceivedAt: searchBase})
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("long"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: id})
	require.NoError(t, err)
	require.True(t, result.Value.IsTextTruncated)
	require.Len(t, result.Value.Text, outlookmail.MaxTextBytes)
	require.Equal(t, strings.Repeat("é", outlookmail.MaxTextBytes/2), result.Value.Text)
}

func TestGetMessageMissingOrMalformedIDSelectsNotFoundOrDefect(t *testing.T) {
	fake := newGraphFake(t)
	client, _ := delegatedClient(t, fake)
	result, err := sdkgo.RunQuery(newOutlookDexContext("missing"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: "AAMkMessage-404_gone="})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchNotFound, result.Branch)
	requireNoSecretsOrServerText(t, result.Failure)
	result, err = sdkgo.RunQuery(newOutlookDexContext("malformed"), client.GetMessage(), outlookConnection, outlookmail.GetMessageInput{MessageID: "id with spaces"})
	require.NoError(t, err)
	require.Equal(t, outlookmail.GetMessageBranchDefect, result.Branch)
	require.Equal(t, 1, fake.RequestCount(graphtest.EndpointGetMessage))
}
