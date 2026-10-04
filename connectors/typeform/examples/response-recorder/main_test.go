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
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	responserecorder "github.com/superdurable/dex-connectors-library/connectors/typeform/examples/response-recorder/flow"
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

// TestEndpointAnswersSubmissionsWhileDexIsUnreachable proves the endpoint verifies and filters submissions without Dex.
func TestEndpointAnswersSubmissionsWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	logs := newRecordedLogs(t)
	connection := newExampleConnection(t, typeform.WithLogger(logs.logger()))
	flow := responserecorder.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	endpoint.start(t)
	recorded := recordedFormID + ":token0001"

	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0001", true), sentinelSecret, false))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0002", true), sentinelSecret, true))
	require.Equal(t, http.StatusBadRequest, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0003", true), "another-secret", false))
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, otherFormID, "token0004", true), sentinelSecret, false),
		"the binding filters another form but still acknowledges it")
	require.Equal(t, http.StatusOK, endpoint.deliverWebhook(t,
		submissionBody("form_response_partial", recordedFormID, "token0005", true), sentinelSecret, false),
		"a partial response is acknowledged without an event")
	require.Eventually(t, func() bool {
		return len(logs.find("trigger delivery failed; retrying", map[string]string{"event_id": recorded})) > 0
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged submission is retried toward Dex")
	for _, token := range []string{"token0002", "token0003", "token0004", "token0005"} {
		require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": recordedFormID + ":" + token}), token)
	}
	require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": otherFormID + ":token0004"}))
	require.NotContains(t, logs.text(), sentinelSecret)
	require.NotContains(t, logs.text(), sentinelToken)
	require.NotContains(t, logs.text(), "ada@example.com", "records carry event IDs, never answers")
}
