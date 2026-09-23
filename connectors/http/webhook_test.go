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
)

type replayGuard struct {
	mu   sync.Mutex
	used map[string]bool
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
