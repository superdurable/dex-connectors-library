// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package email_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/email/internal/mailtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testUsername = "support@example.com"
	testPassword = "app-password-SECRET-0123456789"
)

var emailConnection = sdkgo.ConnectionRef{Provider: "email", Name: "email-test"}

// mailFixture is one IMAP and one SMTP test server sharing a throwaway certificate authority.
type mailFixture struct {
	certificates *mailtest.Certificates
	imap         *mailtest.IMAPServer
	smtp         *mailtest.SMTPServer
	config       email.Config
}

func newMailFixture(t *testing.T) *mailFixture {
	t.Helper()
	return newMailFixtureWithSecurity(t, mailtest.ImplicitTLS, mailtest.ImplicitTLS)
}

func newMailFixtureWithSecurity(t *testing.T, imapSecurity mailtest.Security, smtpSecurity mailtest.Security) *mailFixture {
	t.Helper()
	certificates := mailtest.NewCertificates(t)
	fixture := &mailFixture{certificates: certificates}
	fixture.imap = mailtest.StartIMAPServer(t, mailtest.IMAPServerConfig{
		Certificates: certificates, Security: imapSecurity, Username: testUsername, Password: testPassword, Mailboxes: []string{"Archive"},
	})
	fixture.smtp = mailtest.StartSMTPServer(t, mailtest.SMTPServerConfig{
		Certificates: certificates, Security: smtpSecurity, Username: testUsername, Password: testPassword,
	})
	fixture.config = email.Config{
		IMAPHost: fixture.imap.Host, IMAPPort: int64(fixture.imap.Port), IMAPSecurity: imapSecurityFor(imapSecurity),
		SMTPHost: fixture.smtp.Host, SMTPPort: int64(fixture.smtp.Port), SMTPSecurity: smtpSecurityFor(smtpSecurity),
		FromName: "Acme Support",
	}
	return fixture
}

func (fixture *mailFixture) client(t *testing.T) *email.Client {
	t.Helper()
	return fixture.clientWithCredentials(t, email.Credentials{Username: testUsername, Password: sdkgo.NewSecretString(testPassword)})
}

func (fixture *mailFixture) clientWithCredentials(t *testing.T, credentials email.Credentials) *email.Client {
	t.Helper()
	client, err := email.New(fixture.config, sdkgo.StaticCredentialProvider[email.Credentials]{emailConnection: credentials},
		email.WithTLSRootCAs(fixture.certificates.RootCAs))
	require.NoError(t, err)
	return client
}

// appendMessage seeds INBOX and returns the reference searchMessages would return.
func (fixture *mailFixture) appendMessage(t *testing.T, mailbox string, raw string, flags []imap.Flag, receivedAt time.Time) email.MessageReference {
	t.Helper()
	uid := fixture.imap.AppendMessage(t, mailbox, []byte(raw), flags, receivedAt)
	return email.MessageReference{Mailbox: mailbox, UIDValidity: fixture.imap.UIDValidity(t, mailbox), UID: uint32(uid)}
}

func imapSecurityFor(security mailtest.Security) email.IMAPSecurity {
	if security == mailtest.ImplicitTLS {
		return email.IMAPSecurityImplicitTLS
	}
	return email.IMAPSecurityStartTLS
}

func smtpSecurityFor(security mailtest.Security) email.SMTPSecurity {
	if security == mailtest.ImplicitTLS {
		return email.SMTPSecurityImplicitTLS
	}
	return email.SMTPSecurityStartTLS
}

// plainMessage builds a one-part text/plain RFC 5322 message with CRLF line endings.
func plainMessage(from string, to string, subject string, messageID string, body string, extraHeaders ...string) string {
	lines := []string{
		"From: " + from, "To: " + to, "Subject: " + subject, "Date: Wed, 28 Jan 2026 09:12:00 +0000",
		"Message-ID: <" + messageID + ">", "MIME-Version: 1.0", "Content-Type: text/plain; charset=utf-8",
	}
	lines = append(lines, extraHeaders...)
	return strings.Join(lines, "\r\n") + "\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
}

// emailDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type emailDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newEmailDexContext(step string) *emailDexContext {
	return &emailDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *emailDexContext) nextAttempt() *emailDexContext {
	return &emailDexContext{
		Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1,
	}
}

func (*emailDexContext) FlowID() string                          { return "email-flow" }
func (*emailDexContext) RunID() string                           { return "run" }
func (*emailDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *emailDexContext) StepExecutionID() string         { return context.step }
func (*emailDexContext) FromStepExecutionID() string             { return "" }
func (*emailDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*emailDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *emailDexContext) Attempt() int32                  { return context.attempt }
func (*emailDexContext) HasTimerFired() bool                     { return false }
func (*emailDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*emailDexContext) WaitForMethodFailed() bool               { return false }
func (*emailDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*emailDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*emailDexContext) RecordEvent(string, any) error { return nil }

func (context *emailDexContext) RecordHeartbeat(value any) error {
	if context.rejectsHeartbeat {
		return errors.New("heartbeat stream closed")
	}
	context.heartbeatCount++
	if value == nil {
		context.recordedHeartbeat = nil
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	context.recordedHeartbeat = encoded
	return nil
}

func (context *emailDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

// requireRetry asserts err is a connector Retry of kind and returns its message.
func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) string {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind, retry.Failure.Message)
	return retry.Failure.Message
}

// requireNoSecretsOrServerText checks a Failure never repeats the password or the servers' refusal text.
func requireNoSecretsOrServerText(t *testing.T, failure *sdkgo.Failure) {
	t.Helper()
	require.NotNil(t, failure)
	encoded := fmt.Sprintf("%+v", *failure)
	require.NotContains(t, encoded, testPassword)
	require.NotContains(t, encoded, mailtest.SentinelText)
}

var _ dex.Context = (*emailDexContext)(nil)
