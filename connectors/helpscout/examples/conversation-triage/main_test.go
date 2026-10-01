// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
)

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, test := range []struct {
		levelName      string
		isDebugEnabled bool
		hasWarning     bool
	}{{"", false, false}, {"debug", true, false}, {" WARN ", false, false}, {"verbose", false, true}} {
		var output bytes.Buffer
		logger := newLogger(&output, test.levelName)
		require.Equal(t, test.isDebugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), test.levelName)
		require.Equal(t, test.hasWarning, bytes.Contains(output.Bytes(), []byte("LOG_LEVEL is not debug")), test.levelName)
	}
}

// TestRunRecordsDeliveriesWhileDexIsUnreachable proves the endpoint acknowledges only what is on disk.
func TestRunRecordsDeliveriesWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	setup := newExampleSetup(t, unreachableDex)
	running := setup.startExample(t)

	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(601, triagedInboxID, "active"), sentinelWebhookSecret, false))
	pending := setup.pendingEventIDs(t)
	require.Len(t, pending, 1, "the 200 came after the inbox write")
	require.Regexp(t, `^convo\.created:601:[0-9a-f]{32}$`, pending[0])
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(602, triagedInboxID, "active"), sentinelWebhookSecret, true))
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(603, triagedInboxID, "active"), "another-secret", false))
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(604, otherInboxID, "active"), sentinelWebhookSecret, false),
		"the binding filters another inbox but still acknowledges it")
	require.Equal(t, pending, setup.pendingEventIDs(t))
	require.Eventually(t, func() bool { return len(setup.logs.find("dex server unavailable; retrying", nil)) > 0 },
		10*time.Second, 25*time.Millisecond)

	running.stop(t)
	require.Equal(t, pending, setup.pendingEventIDs(t), "an undelivered event survives the shutdown")
	require.NotContains(t, setup.logs.text(), sentinelWebhookSecret)
	require.NotContains(t, setup.logs.text(), sentinelToken)
}
