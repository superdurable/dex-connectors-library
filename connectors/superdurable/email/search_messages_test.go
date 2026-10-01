// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var januaryTwentyEighth = time.Date(2026, 1, 28, 9, 0, 0, 0, time.UTC)

func TestSearchMessagesFiltersAndReturnsNewestFirst(t *testing.T) {
	fixture := newMailFixture(t)
	older := fixture.appendMessage(t, "INBOX", plainMessage("Jane Smith <jane@acme.example.com>", testUsername,
		"Refund request for order 88213", "older@acme.example.com", "First message."), []imap.Flag{imap.FlagSeen}, januaryTwentyEighth.Add(-48*time.Hour))
	fixture.appendMessage(t, "INBOX", plainMessage("Ben <ben@meridian.example.com>", testUsername,
		"Refund request", "ben@meridian.example.com", "Other customer."), nil, januaryTwentyEighth.Add(-time.Hour))
	newer := fixture.appendMessage(t, "INBOX", plainMessage("Jane Smith <jane@acme.example.com>", testUsername,
		"Re: Refund request for order 88213", "newer@acme.example.com", "Second message.",
		"In-Reply-To: <reply@example.com>", "Reply-To: billing@acme.example.com", "Cc: Finance <finance@acme.example.com>"), nil, januaryTwentyEighth)

	result, err := sdkgo.RunQuery(newEmailDexContext("search"), fixture.client(t).SearchMessages(), emailConnection,
		email.SearchMessagesInput{From: "jane@acme.example.com", SubjectContains: "refund"})
	require.NoError(t, err)
	require.Equal(t, email.SearchMessagesBranchSearched, result.Branch, result.Failure)
	require.Equal(t, "INBOX", result.Value.Mailbox)
	require.Equal(t, newer.UIDValidity, result.Value.UIDValidity)
	require.Equal(t, 2, result.Value.TotalMatched)
	require.False(t, result.Value.HasMore)
	require.Len(t, result.Value.Messages, 2)
	latest := result.Value.Messages[0]
	require.Equal(t, newer, latest.Reference, "the highest UID comes first")
	require.Equal(t, "newer@acme.example.com", latest.MessageID)
	require.Equal(t, "reply@example.com", latest.InReplyTo)
	require.Equal(t, email.EmailAddress{Name: "Jane Smith", Address: "jane@acme.example.com"}, latest.From)
	require.Equal(t, []email.EmailAddress{{Address: "billing@acme.example.com"}}, latest.ReplyTo)
	require.Equal(t, []email.EmailAddress{{Name: "Finance", Address: "finance@acme.example.com"}}, latest.Cc)
	require.Equal(t, "Re: Refund request for order 88213", latest.Subject)
	require.Equal(t, time.Date(2026, 1, 28, 9, 12, 0, 0, time.UTC), latest.SentAt)
	require.Equal(t, januaryTwentyEighth, latest.ReceivedAt)
	require.False(t, latest.IsSeen)
	require.Positive(t, latest.SizeBytes)
	require.Equal(t, older, result.Value.Messages[1].Reference)
	require.True(t, result.Value.Messages[1].IsSeen)
}

func TestSearchMessagesBoundsTheResultAndContinuesByUID(t *testing.T) {
	fixture := newMailFixture(t)
	var references []email.MessageReference
	for index := 0; index < 5; index++ {
		references = append(references, fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername,
			fmt.Sprintf("Message %d", index), fmt.Sprintf("m%d@acme.example.com", index), "Body."), nil, januaryTwentyEighth))
	}
	client := fixture.client(t)

	first, err := sdkgo.RunQuery(newEmailDexContext("first-page"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, 5, first.Value.TotalMatched)
	require.True(t, first.Value.HasMore)
	require.Equal(t, []email.MessageReference{references[4], references[3]}, summaryReferences(first.Value.Messages))
	require.Equal(t, references[3].UID, first.Value.NextOlderThanUID)

	second, err := sdkgo.RunQuery(newEmailDexContext("second-page"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{
		Limit: 2, OlderThanUID: first.Value.NextOlderThanUID, UIDValidity: first.Value.UIDValidity,
	})
	require.NoError(t, err)
	require.Equal(t, []email.MessageReference{references[2], references[1]}, summaryReferences(second.Value.Messages))

	stale, err := sdkgo.RunQuery(newEmailDexContext("stale-page"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{
		OlderThanUID: first.Value.NextOlderThanUID, UIDValidity: first.Value.UIDValidity + 1,
	})
	require.NoError(t, err)
	require.Equal(t, email.SearchMessagesBranchProviderRejected, stale.Branch)
	require.Equal(t, sdkgo.FailureConflict, stale.Failure.Kind)
}

func TestSearchMessagesMatchesUnseenDatesAndNonASCIISubjects(t *testing.T) {
	fixture := newMailFixture(t)
	fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Read already", "seen@acme.example.com", "Body."),
		[]imap.Flag{imap.FlagSeen}, januaryTwentyEighth)
	unseen := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername,
		"=?utf-8?q?R=C3=BCckerstattung_bitte?=", "unseen@acme.example.com", "Body."), nil, januaryTwentyEighth)
	fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Too old", "old@acme.example.com", "Body."),
		nil, januaryTwentyEighth.AddDate(0, 0, -10))
	client := fixture.client(t)

	result, err := sdkgo.RunQuery(newEmailDexContext("unseen"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{
		IsUnseen: true, SinceDate: "2026-01-27", BeforeDate: "2026-01-29",
	})
	require.NoError(t, err)
	require.Equal(t, []email.MessageReference{unseen}, summaryReferences(result.Value.Messages))
	require.Equal(t, "Rückerstattung bitte", result.Value.Messages[0].Subject)

	byUTF8Subject, err := sdkgo.RunQuery(newEmailDexContext("utf8"), client.SearchMessages(), emailConnection,
		email.SearchMessagesInput{SubjectContains: "Rückerstattung"})
	require.NoError(t, err)
	require.Equal(t, []email.MessageReference{unseen}, summaryReferences(byUTF8Subject.Value.Messages))
}

func TestSearchMessagesRejectsInvalidInputWithoutConnecting(t *testing.T) {
	fixture := newMailFixture(t)
	client := fixture.client(t)
	for name, input := range map[string]email.SearchMessagesInput{
		"limit above maximum":          {Limit: email.MaxSearchLimit + 1},
		"negative limit":               {Limit: -1},
		"line break in from":           {From: "jane@acme.example.com\r\nX: y"},
		"surrounding spaces":           {SubjectContains: " refund"},
		"bad date":                     {SinceDate: "28/01/2026"},
		"before not after since":       {SinceDate: "2026-01-28", BeforeDate: "2026-01-28"},
		"continuation without UIDs":    {OlderThanUID: 10},
		"uidValidity without olderUID": {UIDValidity: 7},
		"wildcard mailbox":             {Mailbox: "INBOX*"},
	} {
		result, err := sdkgo.RunQuery(newEmailDexContext("invalid"), client.SearchMessages(), emailConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, email.SearchMessagesBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Zero(t, fixture.imap.LoginCount())
}

func TestSearchMessagesMapsServerRefusals(t *testing.T) {
	fixture := newMailFixture(t)
	missing, err := sdkgo.RunQuery(newEmailDexContext("missing"), fixture.client(t).SearchMessages(), emailConnection,
		email.SearchMessagesInput{Mailbox: "Projects"})
	require.NoError(t, err)
	require.Equal(t, email.SearchMessagesBranchProviderRejected, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)

	wrongPassword := fixture.clientWithCredentials(t, email.Credentials{Username: testUsername, Password: sdkgo.NewSecretString("wrong-password")})
	refused, err := sdkgo.RunQuery(newEmailDexContext("refused"), wrongPassword.SearchMessages(), emailConnection, email.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, email.SearchMessagesBranchProviderRejected, refused.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, refused.Failure.Kind)
	require.Equal(t, "the IMAP server answered LOGIN with NO [AUTHENTICATIONFAILED]", refused.Failure.Message)
	requireNoSecretsOrServerText(t, refused.Failure)
}

func TestIMAPConnectionsRequireAVerifiedTLSServer(t *testing.T) {
	t.Run("startTLS", func(t *testing.T) {
		fixture := newMailFixtureWithSecurity(t, mailtest.StartTLS, mailtest.StartTLS)
		result, err := sdkgo.RunQuery(newEmailDexContext("starttls"), fixture.client(t).SearchMessages(), emailConnection, email.SearchMessagesInput{})
		require.NoError(t, err)
		require.Equal(t, email.SearchMessagesBranchSearched, result.Branch, result.Failure)
	})
	t.Run("server without STARTTLS", func(t *testing.T) {
		fixture := newMailFixtureWithSecurity(t, mailtest.PlaintextOnly, mailtest.ImplicitTLS)
		result, err := sdkgo.RunQuery(newEmailDexContext("plaintext"), fixture.client(t).SearchMessages(), emailConnection, email.SearchMessagesInput{})
		require.NoError(t, err)
		require.Equal(t, email.SearchMessagesBranchProviderRejected, result.Branch)
		require.Contains(t, result.Failure.Message, "did not accept STARTTLS")
		require.Zero(t, fixture.imap.LoginCount(), "no credential is sent over plaintext")
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		fixture := newMailFixture(t)
		client, err := email.New(fixture.config, sdkgo.StaticCredentialProvider[email.Credentials]{
			emailConnection: {Username: testUsername, Password: sdkgo.NewSecretString(testPassword)},
		})
		require.NoError(t, err)
		result, err := sdkgo.RunQuery(newEmailDexContext("untrusted"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{})
		require.NoError(t, err)
		require.Equal(t, email.SearchMessagesBranchProviderRejected, result.Branch)
		require.Equal(t, "the IMAP server certificate failed verification for the configured host", result.Failure.Message)
		require.Zero(t, fixture.imap.LoginCount())
	})
	t.Run("implicit TLS against a STARTTLS port", func(t *testing.T) {
		fixture := newMailFixtureWithSecurity(t, mailtest.StartTLS, mailtest.ImplicitTLS)
		fixture.config.IMAPSecurity = email.IMAPSecurityImplicitTLS
		result, err := sdkgo.RunQuery(newEmailDexContext("wrong-mode"), fixture.client(t).SearchMessages(), emailConnection, email.SearchMessagesInput{})
		require.NoError(t, err)
		require.Equal(t, email.SearchMessagesBranchProviderRejected, result.Branch)
		require.Contains(t, result.Failure.Message, "did not answer with TLS")
	})
	t.Run("server not listening", func(t *testing.T) {
		fixture := newMailFixture(t)
		fixture.config.IMAPPort = 1
		_, err := sdkgo.RunQuery(newEmailDexContext("refused"), fixture.client(t).SearchMessages(), emailConnection, email.SearchMessagesInput{})
		requireRetry(t, err, sdkgo.FailureTransport)
	})
}

func summaryReferences(summaries []email.MessageSummary) []email.MessageReference {
	references := make([]email.MessageReference, len(summaries))
	for index, summary := range summaries {
		references[index] = summary.Reference
	}
	return references
}
