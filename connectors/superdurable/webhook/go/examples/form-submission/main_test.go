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
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	formsubmission "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook/examples/form-submission/flow"
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
	connection := newExampleConnection(t, "https://127.0.0.1:1/forward", webhook.WithLogger(logs.logger()))
	flow := formsubmission.NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, errors.Join(client.Close(), cache.Close())) })
	endpoint := newSubmissionEndpoint(t, connection, newSubmissionTarget(client, flow, logs.logger()))
	endpoint.start(t)

	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, submissionBody("evt_offline", "form_response"), false))
	require.Equal(t, http.StatusBadRequest, endpoint.postSubmission(t, submissionBody("evt_tampered", "form_response"), true))
	require.Equal(t, http.StatusOK, endpoint.postSubmission(t, submissionBody("evt_partial", "form_response_partial"), false),
		"the binding filters a partial response but still acknowledges it")
	require.Eventually(t, func() bool {
		return len(logs.find("trigger delivery failed; retrying", map[string]string{"event_id": "evt_offline"})) > 0
	}, 10*time.Second, 25*time.Millisecond, "the acknowledged submission is retried toward Dex")
	require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": "evt_partial"}))
	require.Empty(t, logs.find("trigger delivery failed; retrying", map[string]string{"event_id": "evt_tampered"}))
	require.NotContains(t, logs.text(), sentinelSecret)
}
