// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testMailbox      = "support@contoso.example"
	testClientSecret = "test-client-secret-not-an-entra-value"
	testRefreshToken = "initial-refresh-token"
	testTenantID     = "contoso.onmicrosoft.com"
	testCustomer     = "jane@acme.example.com"

	// testClientIDPrefix and testClientIDSuffix keep the GUID fixture out of any single literal.
	testClientIDPrefix = "6731de76-14a6-49ae"
	testClientIDSuffix = "-8b2e-0ec0c2d8d5a1"
)

var outlookConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "outlook-mail-test"}

func testClientID() string { return testClientIDPrefix + testClientIDSuffix }

func newGraphFake(t *testing.T) *graphtest.Server {
	t.Helper()
	return graphtest.Start(t, graphtest.ServerConfig{
		MailboxAddress: testMailbox, ClientID: testClientID(), ClientSecret: testClientSecret,
		RefreshToken: testRefreshToken, TenantID: testTenantID,
	})
}

// testCredentialHost holds a test connection's credentials the way an application credential store does.
type testCredentialHost = graphtest.CredentialHost[outlookmail.Credentials]

// delegatedClient connects as Dex Web leaves a delegated connection after consent: tokens valid for an hour.
func delegatedClient(t *testing.T, fake *graphtest.Server, options ...outlookmail.Option) (*outlookmail.Client, *testCredentialHost) {
	t.Helper()
	consented := delegatedCredentials()
	consented.AccessToken = sdkgo.NewSecretString(fake.IssueDelegatedAccessToken())
	expiresAt := time.Now().Add(time.Hour)
	credentials := graphtest.NewCredentialHost(consented, &expiresAt)
	return newHostedClient(t, fake, outlookmail.Config{}, credentials, options...), credentials
}

// appOnlyClient connects with client credentials and no stored token, as Dex Web saves the app-only form.
func appOnlyClient(t *testing.T, fake *graphtest.Server, mailbox string) (*outlookmail.Client, *testCredentialHost) {
	t.Helper()
	credentials := graphtest.NewCredentialHost(outlookmail.Credentials{
		AuthMethodID: outlookmail.AppOnlyAuthMethodID, TenantID: testTenantID, ClientID: testClientID(),
		ClientSecret: sdkgo.NewSecretString(testClientSecret),
	}, nil)
	return newHostedClient(t, fake, outlookmail.Config{Mailbox: mailbox}, credentials), credentials
}

func newHostedClient(t *testing.T, fake *graphtest.Server, config outlookmail.Config, credentials *testCredentialHost, options ...outlookmail.Option) *outlookmail.Client {
	t.Helper()
	client, err := outlookmail.New(config, credentials, append([]outlookmail.Option{outlookmail.WithLocalProviderURL(fake.URL)}, options...)...)
	require.NoError(t, err)
	return client
}

func seedCustomerMessage(fake *graphtest.Server, subject string, receivedAt time.Time) string {
	return fake.AddMessage(graphtest.SeedMessage{
		FromName: "Jane Smith", FromAddress: testCustomer, To: []string{testMailbox}, Subject: subject,
		Body: "I was charged twice for order 88213.", ReceivedAt: receivedAt,
	})
}

// outlookDexContext is a dex.Context whose heartbeat reads the previous attempt's value, as Dex supplies it.
type outlookDexContext struct {
	context.Context
	step              string
	previousHeartbeat json.RawMessage
	recordedHeartbeat json.RawMessage
	heartbeatCount    int
	rejectsHeartbeat  bool
	attempt           int32
}

func newOutlookDexContext(step string) *outlookDexContext {
	return &outlookDexContext{Context: context.Background(), step: step, attempt: 1}
}

// nextAttempt is the following attempt of the same Step execution, which sees this attempt's last heartbeat.
func (context *outlookDexContext) nextAttempt() *outlookDexContext {
	return &outlookDexContext{Context: context.Context, step: context.step, previousHeartbeat: context.recordedHeartbeat, attempt: context.attempt + 1}
}

func (*outlookDexContext) FlowID() string                          { return "outlook-mail-flow" }
func (*outlookDexContext) RunID() string                           { return "run" }
func (*outlookDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *outlookDexContext) StepExecutionID() string         { return context.step }
func (*outlookDexContext) FromStepExecutionID() string             { return "" }
func (*outlookDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*outlookDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (context *outlookDexContext) Attempt() int32                  { return context.attempt }
func (*outlookDexContext) HasTimerFired() bool                     { return false }
func (*outlookDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*outlookDexContext) WaitForMethodFailed() bool               { return false }
func (*outlookDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*outlookDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*outlookDexContext) RecordEvent(string, any) error { return nil }

func (context *outlookDexContext) RecordHeartbeat(value any) error {
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

func (context *outlookDexContext) GetLastHeartbeatValue(target any) (bool, error) {
	if context.previousHeartbeat == nil {
		return false, nil
	}
	return true, json.Unmarshal(context.previousHeartbeat, target)
}

// requireRetry asserts err is a connector Retry of kind and returns its message and delay.
func requireRetry(t *testing.T, err error, kind sdkgo.FailureKind) (string, time.Duration) {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, kind, retry.Failure.Kind, retry.Failure.Message)
	var retryAfter *dex.RetryAfterError
	if errors.As(err, &retryAfter) {
		return retry.Failure.Message, retryAfter.After
	}
	return retry.Failure.Message, 0
}

// requireNoSecretsOrServerText checks a Failure never repeats a credential or Graph's message text.
func requireNoSecretsOrServerText(t *testing.T, failure *sdkgo.Failure) {
	t.Helper()
	require.NotNil(t, failure)
	encoded := fmt.Sprintf("%+v", *failure)
	require.NotContains(t, encoded, testClientSecret)
	require.NotContains(t, encoded, testRefreshToken)
	require.NotContains(t, encoded, "access-")
	require.NotContains(t, encoded, graphtest.SentinelText)
}

var _ dex.Context = (*outlookDexContext)(nil)
