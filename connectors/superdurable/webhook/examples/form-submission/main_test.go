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
	setup := newExampleSetup(t, "https://127.0.0.1:1/forward", unreachableDex)
	running := setup.startExample(t)

	require.Equal(t, http.StatusOK, setup.postSubmission(t, submissionBody("evt_offline", "form_response"), false))
	require.Equal(t, []string{"evt_offline"}, setup.pendingEventIDs(t), "the 200 came after the inbox write")
	require.Equal(t, http.StatusBadRequest, setup.postSubmission(t, submissionBody("evt_tampered", "form_response"), true))
	require.Equal(t, http.StatusOK, setup.postSubmission(t, submissionBody("evt_partial", "form_response_partial"), false),
		"the binding filters a partial response but still acknowledges it")
	require.Equal(t, []string{"evt_offline"}, setup.pendingEventIDs(t))
	require.Eventually(t, func() bool { return len(setup.logs.find("dex server unavailable; retrying", nil)) > 0 },
		10*time.Second, 25*time.Millisecond)

	running.stop(t)
	require.Equal(t, []string{"evt_offline"}, setup.pendingEventIDs(t), "an undelivered event survives the shutdown")
	require.NotContains(t, setup.logs.text(), sentinelSecret)
}
