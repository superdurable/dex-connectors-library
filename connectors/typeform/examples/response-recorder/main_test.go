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
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
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

// TestRunRecordsSubmissionsWhileDexIsUnreachable proves the endpoint acknowledges only what is on disk.
func TestRunRecordsSubmissionsWhileDexIsUnreachable(t *testing.T) {
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	setup := newExampleSetup(t, unreachableDex)
	running := setup.startExample(t)
	recorded := recordedFormID + ":token0001"

	require.Equal(t, http.StatusOK, setup.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0001", true), sentinelSecret, false))
	require.Equal(t, []string{recorded}, setup.pendingEventIDs(t), "the 200 came after the inbox write")
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0002", true), sentinelSecret, true))
	require.Equal(t, http.StatusBadRequest, setup.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, recordedFormID, "token0003", true), "another-secret", false))
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t,
		submissionBody(typeform.WebhookEventTypeFormResponse, otherFormID, "token0004", true), sentinelSecret, false),
		"the binding filters another form but still acknowledges it")
	require.Equal(t, http.StatusOK, setup.deliverWebhook(t,
		submissionBody("form_response_partial", recordedFormID, "token0005", true), sentinelSecret, false),
		"a partial response is acknowledged without a record")
	require.Equal(t, []string{recorded}, setup.pendingEventIDs(t))
	require.Eventually(t, func() bool { return len(setup.logs.find("dex server unavailable; retrying", nil)) > 0 },
		10*time.Second, 25*time.Millisecond)

	running.stop(t)
	require.Equal(t, []string{recorded}, setup.pendingEventIDs(t), "an undelivered submission survives the shutdown")
	require.NotContains(t, setup.logs.text(), sentinelSecret)
	require.NotContains(t, setup.logs.text(), sentinelToken)
	require.NotContains(t, setup.logs.text(), "ada@example.com", "records carry event IDs, never answers")
}
