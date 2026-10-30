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
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	conversationtriage "github.com/superdurable/dex-connectors-library/connectors/helpscout/examples/conversation-triage/flow"
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
// without Dex, and retries the accepted conversation toward Dex.
func TestEndpointAnswersDeliveriesWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, newExampleCredentials(), helpscout.WithLogger(logs.logger()))
	flow := conversationtriage.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newConversationEndpoint(t, connection, newConversationTarget(client, flow, logs.logger()))
	endpoint.start(t)

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(601, triagedInboxID, "active"), sentinelWebhookSecret, false))
	var accepted string
	require.Eventually(t, func() bool {
		for _, retry := range logs.find("trigger delivery failed; retrying", nil) {
			accepted = retry.attrs["event_id"]
			return true
		}
		return false
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged conversation is retried toward Dex")
	require.Regexp(t, `^convo\.created:601:[0-9a-f]{32}$`, accepted)
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(602, triagedInboxID, "active"), sentinelWebhookSecret, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(603, triagedInboxID, "active"), "another-secret", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t, helpscout.WebhookEventConversationCreated,
		conversationWebhookBody(604, otherInboxID, "active"), sentinelWebhookSecret, false),
		"the binding filters another inbox but still acknowledges it")
	for _, retry := range logs.find("trigger delivery failed; retrying", nil) {
		require.Equal(t, accepted, retry.attrs["event_id"], "only the accepted conversation reaches the target")
	}
	require.NotContains(t, logs.text(), sentinelWebhookSecret)
	require.NotContains(t, logs.text(), sentinelToken)
}
