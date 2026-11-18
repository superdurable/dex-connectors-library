// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// unavailableCredentialBroker fails every resolution as a temporarily unavailable credential broker does.
type unavailableCredentialBroker struct{}

func (unavailableCredentialBroker) Resolve(sdkgo.Call) (email.Credentials, error) {
	return email.Credentials{}, projectconfig.ErrCredentialBrokerUnavailable
}

// failingResolveCredentials fails every resolution with resolveErr.
type failingResolveCredentials struct{ resolveErr error }

func (credentials failingResolveCredentials) Resolve(sdkgo.Call) (email.Credentials, error) {
	return email.Credentials{}, credentials.resolveErr
}

func TestSendMessageClassifiesCredentialResolveFailures(t *testing.T) {
	fixture := newMailFixture(t)
	for name, resolveErr := range map[string]error{
		"broker unavailable": projectconfig.ErrCredentialBrokerUnavailable,
		"not configured":     errors.New("connection is not configured"),
		"deadline":           context.DeadlineExceeded,
		"unlisted":           errors.New("unlisted resolve failure"),
	} {
		client, err := email.New(fixture.config, failingResolveCredentials{resolveErr}, email.WithTLSRootCAs(fixture.certificates.RootCAs))
		require.NoError(t, err)
		_, err = sdkgo.RunMutation(newEmailDexContext("send-transient-"+name), client.SendMessage(), emailConnection, validSendMessageInput())
		requireRetry(t, err, sdkgo.FailureAvailability)
	}
	for name, resolveErr := range map[string]error{
		"sdk reauthorization":     sdkgo.NewReauthorizationRequiredError(nil),
		"project reauthorization": projectconfig.ErrReauthorizationRequired,
		"object not found":        projectconfig.ErrObjectNotFound,
		"not selected":            projectconfig.ErrConnectionNotSelected,
		"broker rejected":         projectconfig.ErrCredentialBrokerRejected,
	} {
		client, err := email.New(fixture.config, failingResolveCredentials{resolveErr}, email.WithTLSRootCAs(fixture.certificates.RootCAs))
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(newEmailDexContext("send-terminal-"+name), client.SendMessage(), emailConnection, validSendMessageInput())
		require.NoError(t, err, name)
		require.Equal(t, email.SendMessageBranchDefect, result.Branch, name)
	}
	require.Empty(t, fixture.smtp.Submissions(), "nothing is submitted without credentials")
}

func (fixture *mailFixture) clientWithUnavailableBroker(t *testing.T) *email.Client {
	t.Helper()
	client, err := email.New(fixture.config, unavailableCredentialBroker{}, email.WithTLSRootCAs(fixture.certificates.RootCAs))
	require.NoError(t, err)
	return client
}

// clientWithUnreachableIMAP points IMAP at a closed local port, so every IMAP connection fails before login.
func (fixture *mailFixture) clientWithUnreachableIMAP(t *testing.T) *email.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	config := fixture.config
	config.IMAPHost, config.IMAPPort = "127.0.0.1", int64(listener.Addr().(*net.TCPAddr).Port)
	require.NoError(t, listener.Close())
	client, err := email.New(config, sdkgo.StaticCredentialProvider[email.Credentials]{
		emailConnection: {Username: testUsername, Password: sdkgo.NewSecretString(testPassword)},
	}, email.WithTLSRootCAs(fixture.certificates.RootCAs))
	require.NoError(t, err)
	return client
}

func TestOperationsRetryAnUnavailableCredentialBroker(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	client := fixture.clientWithUnavailableBroker(t)

	_, err := sdkgo.RunQuery(newEmailDexContext("search-broker"), client.SearchMessages(), emailConnection, email.SearchMessagesInput{})
	requireRetry(t, err, sdkgo.FailureAvailability)
	_, err = sdkgo.RunQuery(newEmailDexContext("get-broker"), client.GetMessage(), emailConnection, email.GetMessageInput{Message: source})
	requireRetry(t, err, sdkgo.FailureAvailability)
	isTrue := true
	_, err = sdkgo.RunMutation(newEmailDexContext("flags-broker"), client.SetFlags(), emailConnection, email.SetFlagsInput{Message: source, IsSeen: &isTrue})
	requireRetry(t, err, sdkgo.FailureAvailability)
	_, err = sdkgo.RunMutation(newEmailDexContext("move-broker"), client.MoveMessage(), emailConnection,
		email.MoveMessageInput{Message: source, DestinationMailbox: "Archive"})
	requireRetry(t, err, sdkgo.FailureAvailability)

	send := newEmailDexContext("send-broker")
	_, err = sdkgo.RunMutation(send, client.SendMessage(), emailConnection, validSendMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	sent, err := sdkgo.RunMutation(send.nextAttempt(), fixture.client(t).SendMessage(), emailConnection, validSendMessageInput())
	require.NoError(t, err)
	require.Equal(t, email.SendMessageBranchSent, sent.Branch, "the failed attempt is not counted as a submission")
	require.Len(t, fixture.smtp.Submissions(), 1)
}

func TestRepeatSafeWritesRetryATerminalCredentialFailureAfterTheFirstAttempt(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	unusable := fixture.clientWithCredentials(t, email.Credentials{Username: testUsername})
	isTrue := true
	first, err := sdkgo.RunMutation(newEmailDexContext("flags-terminal"), unusable.SetFlags(), emailConnection, email.SetFlagsInput{Message: source, IsSeen: &isTrue})
	require.NoError(t, err)
	require.Equal(t, email.SetFlagsBranchDefect, first.Branch)

	_, err = sdkgo.RunMutation(newEmailDexContext("flags-terminal").nextAttempt(), unusable.SetFlags(), emailConnection,
		email.SetFlagsInput{Message: source, IsSeen: &isTrue})
	requireRetry(t, err, sdkgo.FailureAuthentication)
	_, err = sdkgo.RunMutation(newEmailDexContext("move-terminal").nextAttempt(), unusable.MoveMessage(), emailConnection,
		email.MoveMessageInput{Message: source, DestinationMailbox: "Archive"})
	requireRetry(t, err, sdkgo.FailureAuthentication)
}

func TestReplyToMessageAfterAnAcceptedReplyRetriesTemporaryFailuresWithoutResubmitting(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	input := email.ReplyToMessageInput{Message: source, Text: "Hello."}
	first := newEmailDexContext("accepted-then-transient")
	accepted, err := sdkgo.RunMutation(first, fixture.client(t).ReplyToMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchSent, accepted.Branch)

	unreadable := first.nextAttempt()
	_, err = sdkgo.RunMutation(unreadable, fixture.clientWithUnreachableIMAP(t).ReplyToMessage(), emailConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a temporary IMAP failure leaves the accepted marker for a later attempt")
	require.Zero(t, unreadable.heartbeatCount, "the accepted marker stays the last checkpoint")

	brokerless := first.nextAttempt()
	_, err = sdkgo.RunMutation(brokerless, fixture.clientWithUnavailableBroker(t).ReplyToMessage(), emailConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, brokerless.heartbeatCount)

	// Each failed attempt recorded nothing, so the next attempt still reads the accepted marker.
	rebuilt, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).ReplyToMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchSent, rebuilt.Branch)
	require.Equal(t, accepted.Value.Recipients, rebuilt.Value.Recipients)
	require.Len(t, fixture.smtp.Submissions(), 1, "an accepted reply is never resubmitted")
}

func TestReplyToMessageRetriesATemporaryReadFailureBeforeAnySubmission(t *testing.T) {
	fixture := newMailFixture(t)
	source := fixture.appendMessage(t, "INBOX", plainMessage("jane@acme.example.com", testUsername, "Hello", "hello@acme.example.com", "Body."),
		nil, januaryTwentyEighth)
	input := email.ReplyToMessageInput{Message: source, Text: "Hello."}
	first := newEmailDexContext("transient-read")
	_, err := sdkgo.RunMutation(first, fixture.clientWithUnreachableIMAP(t).ReplyToMessage(), emailConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	sent, err := sdkgo.RunMutation(first.nextAttempt(), fixture.client(t).ReplyToMessage(), emailConnection, input)
	require.NoError(t, err)
	require.Equal(t, email.ReplyToMessageBranchSent, sent.Branch, "the failed read is recorded as no submission")
	require.Len(t, fixture.smtp.Submissions(), 1)
}
