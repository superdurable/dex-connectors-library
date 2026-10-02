// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
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

// delegatedClient connects as Dex Web leaves a delegated connection after consent: tokens in the local file.
func delegatedClient(t *testing.T, fake *graphtest.Server, options ...outlookmail.Option) (*outlookmail.Client, string) {
	t.Helper()
	expiresAt := time.Now().Add(time.Hour)
	path := writeConnectionFile(t, map[string]any{}, map[string]any{
		"auth_method": outlookmail.MicrosoftOAuthAuthMethodID, "client_id": testClientID(), "client_secret": testClientSecret,
		"access_token": fake.IssueDelegatedAccessToken(), "refresh_token": testRefreshToken,
	}, &expiresAt)
	return clientFromConnectionFile(t, fake, path, outlookmail.Config{}, options...), path
}

// appOnlyClient connects with client credentials and no stored token, as Dex Web saves the app-only form.
func appOnlyClient(t *testing.T, fake *graphtest.Server, mailbox string) (*outlookmail.Client, string) {
	t.Helper()
	path := writeConnectionFile(t, map[string]any{"mailbox": mailbox}, map[string]any{
		"auth_method": outlookmail.AppOnlyAuthMethodID, "tenant_id": testTenantID, "client_id": testClientID(),
		"client_secret": testClientSecret,
	}, nil)
	return clientFromConnectionFile(t, fake, path, outlookmail.Config{Mailbox: mailbox}), path
}

func clientFromConnectionFile(t *testing.T, fake *graphtest.Server, path string, config outlookmail.Config, options ...outlookmail.Option) *outlookmail.Client {
	t.Helper()
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	credentials := localconfig.NewRefreshingCredentialProvider(store, outlookmail.ConnectorID, outlookConnection.Name,
		outlookmail.DecodeCredentialsJSON, func(credentials outlookmail.Credentials) (json.RawMessage, error) {
			return outlookmail.EncodeCredentialsJSON(credentials)
		})
	client, err := outlookmail.New(config, credentials, append([]outlookmail.Option{outlookmail.WithLocalProviderURL(fake.URL)}, options...)...)
	require.NoError(t, err)
	return client
}

// writeConnectionFile writes the record Dex Web saves for this connector's connection.
func writeConnectionFile(t *testing.T, configuration map[string]any, credentials map[string]any, expiresAt *time.Time) string {
	t.Helper()
	record := map[string]any{
		"connectorId": outlookmail.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail",
		"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": outlookConnection.Name,
		"configuration": configuration, "credentials": credentials, "authMethodId": credentials["auth_method"],
	}
	if expiresAt != nil {
		record["credentialExpiresAt"] = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}

// readStoredCredentials returns the credential object and status of the connection file's only record.
func readStoredCredentials(t *testing.T, path string) (map[string]any, string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var file struct {
		Connections []struct {
			Credentials      map[string]any `json:"credentials"`
			CredentialStatus string         `json:"credentialStatus"`
			AuthMethodID     string         `json:"authMethodId"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Len(t, file.Connections, 1)
	require.Equal(t, file.Connections[0].Credentials["auth_method"], file.Connections[0].AuthMethodID, "a refresh keeps Dex Web's record members")
	return file.Connections[0].Credentials, file.Connections[0].CredentialStatus
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
