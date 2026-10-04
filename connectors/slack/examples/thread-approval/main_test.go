// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	threadapproval "github.com/superdurable/dex-connectors-library/connectors/slack/examples/thread-approval/flow"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestNewLoggerUsesLogLevel(t *testing.T) {
	for _, testCase := range []struct {
		levelName    string
		debugEnabled bool
		warning      bool
	}{
		{"", false, false}, {"info", false, false}, {"debug", true, false}, {"DEBUG", true, false},
		{" warn ", false, false}, {"verbose", false, true},
	} {
		var output bytes.Buffer
		logger := newLogger(&output, testCase.levelName)
		require.Equal(t, testCase.debugEnabled, logger.Enabled(context.Background(), slog.LevelDebug), testCase.levelName)
		require.True(t, logger.Enabled(context.Background(), slog.LevelError), testCase.levelName)
		if testCase.warning {
			require.Contains(t, output.String(), `level=WARN msg="LOG_LEVEL is not debug, info, warn, or error; using info" log_level=verbose`)
		} else {
			require.Empty(t, output.String(), testCase.levelName)
		}
	}
	var output bytes.Buffer
	newLogger(&output, "").Info("trigger delivered after retry", "event_id", "Ev1", "attempts", 2)
	require.Regexp(t, `^time=\S+ level=INFO msg="trigger delivered after retry" event_id=Ev1 attempts=2\n$`, output.String())
}

func TestWaitForDexServerRetriesUntilTheServerAnswers(t *testing.T) {
	logs := testlog.NewLogRecorder()
	attempts := 0
	err := waitForDexServer(context.Background(), func(context.Context) (dex.HealthInfo, error) {
		attempts++
		if attempts < 3 {
			return dex.HealthInfo{}, errors.New("connection refused")
		}
		return dex.HealthInfo{Condition: "SERVING"}, nil
	}, logs.Logger())
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	retries := logs.Find("dex server unavailable; retrying", nil)
	require.Len(t, retries, 2)
	for index, delay := range []string{"250ms", "500ms"} {
		require.Equal(t, slog.LevelWarn, retries[index].Level)
		require.Equal(t, map[string]string{"attempt": strconv.Itoa(index + 1), "delay": delay, "error": "connection refused"}, retries[index].Attrs)
	}
	recovered := logs.Find("dex server available after retry", nil)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, map[string]string{"attempts": "3"}, recovered[0].Attrs)
}

func TestWaitForDexServerStopsWhenContextEnds(t *testing.T) {
	logs := testlog.NewLogRecorder()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		result <- waitForDexServer(ctx, func(context.Context) (dex.HealthInfo, error) {
			cancel()
			return dex.HealthInfo{}, errors.New("connection refused")
		}, logs.Logger())
	}()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("waitForDexServer did not stop after cancellation")
	}
	require.Empty(t, logs.Records(), "a check that failed because the context ended is not a retry")
}

// TestRunUntilStoppedWaitsForUnreachableDexServerAndStopsCleanly runs the example's Worker and Trigger runner,
// wired as run wires them, while no Dex Server is listening.
func TestRunUntilStoppedWaitsForUnreachableDexServerAndStopsCleanly(t *testing.T) {
	logs := testlog.NewLogRecorder()
	slackAPI := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":false,"error":"not_authed"}`))
	}))
	t.Cleanup(slackAPI.Close)
	reference := sdkgo.ConnectionRef{Provider: "slack", Name: threadapproval.ConnectionName}
	slackClient, err := slack.New(slack.Config{Endpoint: slackAPI.URL}, sdkgo.StaticCredentialProvider[slack.Credentials]{reference: {
		BotToken: sdkgo.NewSecretString("SENTINEL-BOT-TOKEN"), UserToken: sdkgo.NewSecretString("SENTINEL-USER-TOKEN"),
		AppToken: sdkgo.NewSecretString("SENTINEL-APP-TOKEN"),
	}}, slack.WithLogger(logs.Logger()))
	require.NoError(t, err)
	connection, err := slack.NewConnection(slackClient, reference)
	require.NoError(t, err)
	registry, err := dex.NewRegistry([]dex.Flow{threadapproval.NewFlow(connection)})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 1 << 20})
	require.NoError(t, err)
	// A Unix socket that nothing listens on makes the Dex Server unreachable without binding a TCP port.
	unreachableDex := "unix://" + filepath.Join(os.TempDir(), fmt.Sprintf("dex-missing-%d.sock", time.Now().UnixNano()))
	worker, err := dex.NewWorker(registry, cache, dex.WorkerOptions{BindAddress: "127.0.0.1:" + unusedPort(t), FlowServiceAddress: unreachableDex})
	require.NoError(t, err)
	client, err := dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: unreachableDex, WorkerTarget: worker.WorkerTarget()})
	require.NoError(t, err)
	triggerRunner, err := slack.NewMessageTriggerRunner(slack.MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []slack.ChannelThreadCreatedTriggerRoute{{
			BindingName: threadapproval.StartTriggerBinding,
			Configuration: slack.ChannelThreadCreatedTriggerConfiguration{
				ChannelID: "C1", ThreadTriggerMatcher: slack.MessageMatcher{MessageContains: "request approval"},
			},
			Target: sdkgo.TriggerTargetFunc[slack.MessageEvent](func(context.Context, sdkgo.TriggerEvent[slack.MessageEvent]) error {
				return errors.New("the Trigger runner must not start before the Dex Server answers")
			}),
		}},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- runUntilStopped(ctx, worker, triggerRunner, client.HealthCheck, logs.Logger()) }()
	require.Never(t, func() bool { return len(result) > 0 }, 2*time.Second, 50*time.Millisecond,
		"the example must wait for the Dex Server instead of exiting")
	waits := logs.Find("dex server unavailable; retrying", nil)
	require.NotEmpty(t, waits)
	require.Equal(t, slog.LevelWarn, waits[0].Level)
	require.Equal(t, "1", waits[0].Attrs["attempt"])
	require.Equal(t, "250ms", waits[0].Attrs["delay"])
	require.Empty(t, logs.Find("slack socket mode connected", nil), "the Trigger runner waits for the Dex Server")
	require.NotContains(t, logs.Text(), "SENTINEL")
	cancel()
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("the example did not stop after cancellation")
	}
	require.NoError(t, errors.Join(client.Close(), stopWorker(worker), cache.Close()))
}

// unusedPort returns a free local port for a Worker bind address that the test never starts.
func unusedPort(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
