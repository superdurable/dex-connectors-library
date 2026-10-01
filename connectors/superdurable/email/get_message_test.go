// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const multipartWithAttachment = "From: Jane Smith <jane@acme.example.com>\r\n" +
	"To: support@example.com\r\n" +
	"Subject: Invoice question\r\n" +
	"Date: Wed, 28 Jan 2026 09:12:00 +0000\r\n" +
	"Message-ID: <invoice@acme.example.com>\r\n" +
	"References: <first@acme.example.com> <second@acme.example.com>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=outer\r\n" +
	"\r\n" +
	"--outer\r\n" +
	"Content-Type: multipart/alternative; boundary=inner\r\n" +
	"\r\n" +
	"--inner\r\n" +
	"Content-Type: text/plain; charset=iso-8859-1\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"Gr=FC=DFe, the invoice total looks wrong.\r\n" +
	"--inner\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p>HTML version</p>\r\n" +
	"--inner--\r\n" +
	"--outer\r\n" +
	"Content-Type: application/pdf; name=\"invoice-88213.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"invoice-88213.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"JVBERi0xLjQKJcfsj6IKNSAwIG9iago8PC9MZW5ndGggNiAwIFI+PgpzdHJlYW0K\r\n" +
	"--outer--\r\n"

func TestGetMessageReturnsDecodedTextAndAttachmentNamesWithoutMarkingSeen(t *testing.T) {
	fixture := newMailFixture(t)
	reference := fixture.appendMessage(t, "INBOX", multipartWithAttachment, nil, januaryTwentyEighth)

	result, err := sdkgo.RunQuery(newEmailDexContext("get"), fixture.client(t).GetMessage(), emailConnection, email.GetMessageInput{Message: reference})
	require.NoError(t, err)
	require.Equal(t, email.GetMessageBranchFound, result.Branch, result.Failure)
	message := result.Value
	require.Equal(t, reference, message.Reference)
	require.Equal(t, "invoice@acme.example.com", message.MessageID)
	require.Equal(t, []string{"first@acme.example.com", "second@acme.example.com"}, message.References)
	require.Equal(t, "Grüße, the invoice total looks wrong.", message.Text, "ISO-8859-1 quoted-printable is decoded to UTF-8")
	require.Equal(t, email.TextSourcePlain, message.TextSource)
	require.False(t, message.IsTextTruncated)
	require.True(t, message.HasAttachments)
	require.Equal(t, []email.Attachment{{Part: "2", FileName: "invoice-88213.pdf", MediaType: "application/pdf", EncodedSizeBytes: 64}}, message.Attachments)
	require.Equal(t, "invoice@acme.example.com", result.Receipt.ProviderObjectID)
	require.Empty(t, fixture.imap.Messages(t, "INBOX")[0].Flags, "BODY.PEEK leaves the message unseen")
}

func TestGetMessageFallsBackToHTMLTextAndBoundsLongBodies(t *testing.T) {
	fixture := newMailFixture(t)
	htmlOnly := fixture.appendMessage(t, "INBOX", "From: jane@acme.example.com\r\nSubject: HTML only\r\nMessage-ID: <html@acme.example.com>\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n"+
		"<html><head><style>p{color:red}</style></head><body><p>Hello&nbsp;team,</p><p>Order <b>88213</b> &amp; 88214.</p>"+
		"<script>alert(1)</script></body></html>\r\n", nil, januaryTwentyEighth)
	longText := strings.Repeat("ü", email.MaxTextBytes)
	long := fixture.appendMessage(t, "INBOX", "From: jane@acme.example.com\r\nSubject: Long\r\nMessage-ID: <long@acme.example.com>\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: base64\r\n\r\n"+
		base64.StdEncoding.EncodeToString([]byte(longText))+"\r\n", nil, januaryTwentyEighth)
	client := fixture.client(t)

	html, err := sdkgo.RunQuery(newEmailDexContext("html"), client.GetMessage(), emailConnection, email.GetMessageInput{Message: htmlOnly})
	require.NoError(t, err)
	require.Equal(t, email.TextSourceHTML, html.Value.TextSource)
	require.Equal(t, "Hello team,\n\nOrder 88213 & 88214.", html.Value.Text, "a non-breaking space becomes a plain space")
	require.NotContains(t, html.Value.Text, string(rune(0xa0)))

	bounded, err := sdkgo.RunQuery(newEmailDexContext("long"), client.GetMessage(), emailConnection, email.GetMessageInput{Message: long})
	require.NoError(t, err)
	require.True(t, bounded.Value.IsTextTruncated)
	require.Len(t, bounded.Value.Text, email.MaxTextBytes)
	require.Equal(t, strings.Repeat("ü", email.MaxTextBytes/2), bounded.Value.Text, "the cut keeps whole UTF-8 characters")
}

func TestGetMessageSelectsNotFoundForAMissingUIDOrAChangedUIDValidity(t *testing.T) {
	fixture := newMailFixture(t)
	reference := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		[]imap.Flag{imap.FlagSeen}, januaryTwentyEighth)
	client := fixture.client(t)
	for name, missing := range map[string]email.MessageReference{
		"missing UID":         {Mailbox: "INBOX", UIDValidity: reference.UIDValidity, UID: reference.UID + 10},
		"changed UIDVALIDITY": {Mailbox: "INBOX", UIDValidity: reference.UIDValidity + 1, UID: reference.UID},
		"missing mailbox":     {Mailbox: "Projects", UIDValidity: reference.UIDValidity, UID: reference.UID},
	} {
		result, err := sdkgo.RunQuery(newEmailDexContext("missing"), client.GetMessage(), emailConnection, email.GetMessageInput{Message: missing})
		require.NoError(t, err, name)
		require.Equal(t, email.GetMessageBranchNotFound, result.Branch, name)
		require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind, name)
	}
	invalid, err := sdkgo.RunQuery(newEmailDexContext("invalid"), client.GetMessage(), emailConnection, email.GetMessageInput{
		Message: email.MessageReference{Mailbox: "INBOX", UID: reference.UID},
	})
	require.NoError(t, err)
	require.Equal(t, email.GetMessageBranchDefect, invalid.Branch)
	require.Contains(t, invalid.Failure.Message, "uidValidity is required")
}
