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
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
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
	booked := "invitee.created:EVENT0001:INVITEE01"

	require.Equal(t, http.StatusOK, setup.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0001", "INVITEE01", bookedEventTypeURI), sentinelSigningKey, false))
	require.Equal(t, []string{booked}, setup.pendingEventIDs(t), "the 200 came after the inbox write")
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0002", "INVITEE02", bookedEventTypeURI), sentinelSigningKey, true))
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0003", "INVITEE03", bookedEventTypeURI), "another-key", false))
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0004", "INVITEE04", otherEventTypeURI), sentinelSigningKey, false),
		"the binding filters another event type but still acknowledges it")
	require.Equal(t, []string{booked}, setup.pendingEventIDs(t))
	require.Eventually(t, func() bool { return len(setup.logs.find("dex server unavailable; retrying", nil)) > 0 },
		10*time.Second, 25*time.Millisecond)

	running.stop(t)
	require.Equal(t, []string{booked}, setup.pendingEventIDs(t), "an undelivered event survives the shutdown")
	require.NotContains(t, setup.logs.text(), sentinelSigningKey)
	require.NotContains(t, setup.logs.text(), sentinelToken)
}
