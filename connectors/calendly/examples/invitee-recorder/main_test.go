// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	inviteerecorder "github.com/superdurable/dex-connectors-library/connectors/calendly/examples/invitee-recorder/flow"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
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

// TestEndpointAnswersDeliveriesWhileDexIsUnreachable proves the endpoint verifies and filters deliveries
// without Dex, and retries the accepted booking toward Dex.
func TestEndpointAnswersDeliveriesWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, calendly.WithLogger(logs.logger()))
	flow := inviteerecorder.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newInviteeEndpoint(t, connection, newInviteeTarget(client, flow, logs.logger()))
	endpoint.start(t)
	booked := "invitee.created:EVENT0001:INVITEE01"

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0001", "INVITEE01", bookedEventTypeURI), sentinelSigningKey, false))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0002", "INVITEE02", bookedEventTypeURI), sentinelSigningKey, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0003", "INVITEE03", bookedEventTypeURI), "another-key", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		inviteeWebhookBody(calendly.WebhookEventInviteeCreated, "EVENT0004", "INVITEE04", otherEventTypeURI), sentinelSigningKey, false),
		"the binding filters another event type but still acknowledges it")
	require.Eventually(t, func() bool {
		return len(logs.find("trigger delivery failed; retrying", map[string]string{"event_id": booked})) > 0
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged booking is retried toward Dex")
	for _, eventID := range []string{"EVENT0002:INVITEE02", "EVENT0003:INVITEE03", "EVENT0004:INVITEE04"} {
		require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": "invitee.created:" + eventID}), eventID)
	}
	require.NotContains(t, logs.text(), sentinelSigningKey)
	require.NotContains(t, logs.text(), sentinelToken)
}
