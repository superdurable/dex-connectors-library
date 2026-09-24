// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

var gmailConnection = connector.ConnectionRef{Provider: "google", Name: "gmail-send"}

func TestSendMessageUsesPrimarySenderAndStableMessageID(t *testing.T) {
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer gmail-token", request.Header.Get("Authorization"))
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		decoded, err := base64.RawURLEncoding.DecodeString(payload["raw"])
		require.NoError(t, err)
		raw = string(decoded)
		response.Header().Set("X-Goog-Request-Id", "gmail-request")
		_, _ = response.Write([]byte(`{"id":"message-1","threadId":"thread-1"}`))
	}))
	defer server.Close()
	client := newGmailClient(t, server.URL)
	ctx := newGmailDexContext("send-one")
	result, err := connector.RunMutation(ctx, client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Weekly progress", TextBody: "Keep going"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchSent, result.Branch)
	require.Equal(t, "owner@example.com", result.Value.Sender)
	require.Equal(t, "message-1", result.Value.MessageID)
	require.Contains(t, raw, "From: owner@example.com")
	require.Contains(t, raw, "Message-ID: <"+string(result.Receipt.IdempotencyKey)+"@dex.superdurable.dev>")
	require.NotContains(t, raw, "gmail-token")
}

func TestServerFailureIsUncertainAndNeverAutomaticRetry(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		requests++
		response.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newGmailClient(t, server.URL)
	result, err := connector.RunMutation(newGmailDexContext("send-unknown"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, 1, requests)
}

func TestRateLimitIsTheOnlyAutomaticRetryPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "3")
		response.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := newGmailClient(t, server.URL)
	_, err := connector.RunMutation(newGmailDexContext("send-rate-limit"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	require.Error(t, err)
	var retry *connector.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, connector.FailureRateLimit, retry.Failure.Kind)
}

func TestProviderRejectionIsTerminal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	client := newGmailClient(t, server.URL)
	result, err := connector.RunMutation(newGmailDexContext("send-rejected"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchRejected, result.Branch)
	require.NotNil(t, result.Failure)
	require.Equal(t, connector.FailureAuthorization, result.Failure.Kind)
}

func TestMalformedSuccessIsUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"threadId":"thread-without-message"}`))
	}))
	defer server.Close()
	client := newGmailClient(t, server.URL)
	result, err := connector.RunMutation(newGmailDexContext("send-malformed"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchUncertain, result.Branch)
	require.NotNil(t, result.Failure)
	require.Equal(t, connector.FailureProtocol, result.Failure.Kind)
}

func TestRejectsHeaderInjectionBeforeProviderCall(t *testing.T) {
	client := newGmailClient(t, "http://127.0.0.1:1")
	result, err := connector.RunMutation(newGmailDexContext("send-defect"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Hello\r\nBcc: victim@example.com", TextBody: "Update"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchDefect, result.Branch)
}

func TestRejectsDisplayNameAsVerifiedPrimaryEmail(t *testing.T) {
	client, err := gmail.New(gmail.Config{Endpoint: "http://127.0.0.1:1"}, connector.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: connector.NewSecretString("gmail-token"), PrimaryEmail: "Owner <owner@example.com>"},
	})
	require.NoError(t, err)
	result, err := connector.RunMutation(newGmailDexContext("sender-defect"), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchDefect, result.Branch)
}

func newGmailClient(t *testing.T, endpoint string) *gmail.Client {
	t.Helper()
	client, err := gmail.New(gmail.Config{Endpoint: endpoint}, connector.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: connector.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	})
	require.NoError(t, err)
	return client
}

type gmailDexContext struct {
	context.Context
	step string
}

func newGmailDexContext(step string) *gmailDexContext {
	return &gmailDexContext{Context: context.Background(), step: step}
}
func (*gmailDexContext) FlowID() string                                  { return "follow-up-flow" }
func (*gmailDexContext) RunID() string                                   { return "run" }
func (*gmailDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *gmailDexContext) StepExecutionID() string                 { return context.step }
func (*gmailDexContext) FromStepExecutionID() string                     { return "" }
func (*gmailDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*gmailDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*gmailDexContext) Attempt() int32                                  { return 1 }
func (*gmailDexContext) HasTimerFired() bool                             { return false }
func (*gmailDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*gmailDexContext) WaitForMethodFailed() bool                       { return false }
func (*gmailDexContext) RecordHeartbeat(any) error                       { return nil }
func (*gmailDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*gmailDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*gmailDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*gmailDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*gmailDexContext)(nil)
