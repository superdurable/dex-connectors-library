// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package httpconnector_test

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	httpconnector "github.com/superdurable/dex-connectors-library/connectors/http"
	"github.com/superdurable/dex-connectors-library/connectors/http/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type replayGuard struct {
	mu   sync.Mutex
	used map[string]bool
}

func TestVerifyWebhookOperationUsesCredentialAndRejectsReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"event":"customer.created"}`)
	reference := sdkgo.ConnectionRef{Provider: "http", Name: "webhook-test"}
	guard := &replayGuard{used: map[string]bool{}}
	client, err := httpconnector.New(httpconnector.Config{BaseURL: "https://example.com"}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{
		reference: {WebhookSecret: sdkgo.NewSecretString("webhook-secret")},
	}, httpconnector.WithWebhookReplayGuard(guard), httpconnector.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	input := httpconnector.WebhookRequest{
		Timestamp: strconv.FormatInt(now.Unix(), 10),
		Signature: httpconnector.SignWebhook([]byte("webhook-secret"), now, body),
		Body:      body,
	}
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-1", "verify-1"), client.VerifyWebhook(), reference, input)
	require.NoError(t, err)
	require.Equal(t, httpconnector.VerifyWebhookBranchVerified, result.Branch)
	require.True(t, result.Value.Verified)

	replayed, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-1", "verify-2"), client.VerifyWebhook(), reference, input)
	require.NoError(t, err)
	require.Equal(t, httpconnector.VerifyWebhookBranchRejected, replayed.Branch)
	require.Equal(t, sdkgo.FailureProviderRejection, replayed.Failure.Kind)
}

func TestVerifyWebhookOperationRequiresReplayProtection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"event":"customer.created"}`)
	reference := sdkgo.ConnectionRef{Provider: "http", Name: "webhook-test"}
	client, err := httpconnector.New(httpconnector.Config{BaseURL: "https://example.com"}, sdkgo.StaticCredentialProvider[httpconnector.Credentials]{
		reference: {WebhookSecret: sdkgo.NewSecretString("webhook-secret")},
	}, httpconnector.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("flow-1", "verify-1"), client.VerifyWebhook(), reference, httpconnector.WebhookRequest{
		Timestamp: strconv.FormatInt(now.Unix(), 10),
		Signature: httpconnector.SignWebhook([]byte("webhook-secret"), now, body),
		Body:      body,
	})
	require.NoError(t, err)
	require.Equal(t, httpconnector.VerifyWebhookBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
}

func (guard *replayGuard) Use(key string, _ time.Time) bool {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if guard.used[key] {
		return false
	}
	guard.used[key] = true
	return true
}

func TestWebhookSignatureAndReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	body := []byte(`{"event":"customer.created"}`)
	secret := []byte("webhook-secret")
	guard := &replayGuard{used: map[string]bool{}}
	verifier := httpconnector.WebhookVerifier{Secret: secret, Replay: guard, Now: func() time.Time { return now }}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	signature := httpconnector.SignWebhook(secret, now, body)
	require.NoError(t, verifier.Verify(timestamp, signature, body))
	require.ErrorContains(t, verifier.Verify(timestamp, signature, body), "replay")
}
