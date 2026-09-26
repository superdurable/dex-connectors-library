// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package localconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
	"google.golang.org/grpc/codes"
)

type testConfiguration struct {
	Endpoint string `json:"endpoint"`
}

type testCredentials struct {
	AccessToken sdkgo.SecretString
}

type testTriggerConfiguration struct {
	ChannelID string `json:"channelId"`
}

type testOperationConfiguration struct {
	ChannelID string `json:"channelId"`
	Message   string `json:"message"`
}

func TestStoreSnapshotsConfigurationAndReloadsCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://one.example", "token-one", time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)

	var configuration testConfiguration
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://one.example", configuration.Endpoint)

	provider := localconfig.NewCredentialProvider(store, "gmail", "sender", decodeTestCredentials)
	credentials, err := provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
	require.NoError(t, err)
	require.Equal(t, "token-one", credentials.AccessToken.Reveal())

	writeConnections(t, path, "https://two.example", "token-two", time.Now().Add(time.Hour))
	require.NoError(t, store.DecodeConfiguration("gmail", "sender", &configuration))
	require.Equal(t, "https://one.example", configuration.Endpoint)
	credentials, err = provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
	require.NoError(t, err)
	require.Equal(t, "token-two", credentials.AccessToken.Reveal())
}

func TestLoadFromEnvironmentRejectsUnknownFieldsAndExpiredCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"schemaVersion":"connectors.dex.dev/local-connections/v1alpha1","connections":[],"unexpected":true}`), 0o600))
	t.Setenv(localconfig.EnvironmentVariable, path)
	_, err := localconfig.LoadFromEnvironment()
	require.ErrorContains(t, err, "unknown field")

	writeConnections(t, path, "https://example.test", "expired-token", time.Now().Add(-time.Minute))
	store, err := localconfig.LoadFromEnvironment()
	require.NoError(t, err)
	provider := localconfig.NewCredentialProvider(store, "gmail", "sender", decodeTestCredentials)
	_, err = provider.Resolve(sdkgo.Call{Connection: sdkgo.ConnectionRef{Provider: "google", Name: "sender"}})
	require.ErrorContains(t, err, "credentials are expired")
}

func TestLoadFileRejectsSymlinkAndDuplicateConnection(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target.json")
	writeConnections(t, target, "https://example.test", "token", time.Now().Add(time.Hour))
	symlink := filepath.Join(directory, "connections.json")
	require.NoError(t, os.Symlink(target, symlink))
	_, err := localconfig.LoadFile(symlink)
	require.ErrorContains(t, err, "regular file")

	contents, err := os.ReadFile(target)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(contents, &file))
	connections := file["connections"].([]any)
	file["connections"] = append(connections, connections[0])
	contents, err = json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(target, contents, 0o600))
	_, err = localconfig.LoadFile(target)
	require.ErrorContains(t, err, "is duplicated")
}

func TestLoadFileRejectsCredentialFileWithBroadPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	require.NoError(t, os.Chmod(path, 0o644))

	_, err := localconfig.LoadFile(path)
	require.ErrorContains(t, err, "permissions must be 0600")
}

func TestStoreDecodesTriggerBindingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	var file map[string]any
	require.NoError(t, json.Unmarshal(contents, &file))
	file["triggerBindings"] = []any{map[string]any{
		"connectorId": "gmail", "connectionName": "sender", "triggerName": "messageCreated", "bindingName": "approval-start",
		"configuration": map[string]any{"channelId": "C123"},
	}}
	contents, err = json.Marshal(file)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))

	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	var configuration testTriggerConfiguration
	require.NoError(t, store.DecodeTriggerConfiguration("gmail", "sender", "messageCreated", "approval-start", &configuration))
	require.Equal(t, "C123", configuration.ChannelID)
	require.ErrorContains(t, store.DecodeTriggerConfiguration("gmail", "sender", "messageCreated", "missing", &configuration), "is not configured")
}

func TestStoreLoadsIsolatedOperationConfigurationSnapshot(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	reference := sdkgo.ConnectorConfigurationRef{
		ConnectorID: "gmail", ConnectionName: "sender", OperationID: "sendMessage",
		FlowType: "ApprovalFlow", StepType: "SendApproval",
	}
	writeUseConfigurations(t, directory, reference, map[string]any{"channelId": "C123", "message": "Approve?"})

	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(directory, localconfig.UseConfigurationsFileName), store.UseConfigurationsPath())
	loaded, err := localconfig.LoadOperationConfiguration[testOperationConfiguration](store, reference)
	require.NoError(t, err)
	require.Equal(t, reference, loaded.Reference)
	require.Equal(t, testOperationConfiguration{ChannelID: "C123", Message: "Approve?"}, loaded.Value)

	writeUseConfigurations(t, directory, reference, map[string]any{"channelId": "C999", "message": "Changed"})
	loaded, err = localconfig.LoadOperationConfiguration[testOperationConfiguration](store, reference)
	require.NoError(t, err)
	require.Equal(t, "C123", loaded.Value.ChannelID)
}

func TestStoreRejectsInvalidOperationConfigurationSidecar(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	reference := sdkgo.ConnectorConfigurationRef{
		ConnectorID: "gmail", ConnectionName: "sender", OperationID: "sendMessage",
		FlowType: "ApprovalFlow", StepType: "SendApproval",
	}
	writeUseConfigurations(t, directory, reference, map[string]any{"channelId": "C123", "unexpected": true})

	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	_, err = localconfig.LoadOperationConfiguration[testOperationConfiguration](store, reference)
	require.ErrorContains(t, err, "unknown field")

	reference.ConnectionName = "missing"
	writeUseConfigurations(t, directory, reference, map[string]any{"channelId": "C123"})
	_, err = localconfig.LoadFile(path)
	require.ErrorContains(t, err, "unknown connection")
}

func TestDurableTriggerTargetReplaysEventAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	event := sdkgo.TriggerEvent[testTriggerConfiguration]{
		ID: "Ev-pending", OccurredAt: time.Unix(42, 0).UTC(), Payload: testTriggerConfiguration{ChannelID: "C123"},
	}
	firstTarget, err := localconfig.NewDurableTriggerTarget(
		store, "gmail", "sender", "messageCreated", "approval-start",
		sdkgo.TriggerTargetFunc[testTriggerConfiguration](func(context.Context, sdkgo.TriggerEvent[testTriggerConfiguration]) error {
			return nil
		}),
	)
	require.NoError(t, err)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(context.Background(), firstTarget, event))

	var replayed []sdkgo.TriggerEvent[testTriggerConfiguration]
	restartedTarget, err := localconfig.NewDurableTriggerTarget(
		store, "gmail", "sender", "messageCreated", "approval-start",
		sdkgo.TriggerTargetFunc[testTriggerConfiguration](func(_ context.Context, received sdkgo.TriggerEvent[testTriggerConfiguration]) error {
			replayed = append(replayed, received)
			return nil
		}),
	)
	require.NoError(t, err)
	replayer, ok := restartedTarget.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	require.NoError(t, replayer.ReplayTriggerDeliveries(context.Background()))
	require.Equal(t, []sdkgo.TriggerEvent[testTriggerConfiguration]{event}, replayed)
	require.NoError(t, replayer.ReplayTriggerDeliveries(context.Background()))
	require.Len(t, replayed, 1)
}

type durableTestEvent = sdkgo.TriggerEvent[testTriggerConfiguration]

func newDurableTestTarget(t *testing.T, store *localconfig.Store, target func(context.Context, durableTestEvent) error) sdkgo.TriggerTarget[testTriggerConfiguration] {
	t.Helper()
	return newLoggingDurableTestTarget(t, store, nil, target)
}

// newLoggingDurableTestTarget builds the test inbox with logger; a nil logger uses slog.Default().
func newLoggingDurableTestTarget(
	t *testing.T,
	store *localconfig.Store,
	logger *slog.Logger,
	target func(context.Context, durableTestEvent) error,
) sdkgo.TriggerTarget[testTriggerConfiguration] {
	t.Helper()
	durableTarget, err := localconfig.NewDurableTriggerTarget(
		store, "gmail", "sender", "replyReceived", "approval-reply", sdkgo.TriggerTargetFunc[testTriggerConfiguration](target),
		localconfig.WithTriggerLogger(logger),
	)
	require.NoError(t, err)
	return durableTarget
}

// inboxLogAttrs returns the identity every test inbox record carries, plus extra key-value pairs.
func inboxLogAttrs(extra ...string) map[string]string {
	attrs := map[string]string{"connector": "gmail", "connection": "sender", "trigger": "replyReceived", "binding": "approval-reply"}
	for index := 0; index+1 < len(extra); index += 2 {
		attrs[extra[index]] = extra[index+1]
	}
	return attrs
}

// logMessages lists the captured messages in order.
func logMessages(logs *testsupport.LogRecorder) []string {
	messages := []string{}
	for _, record := range logs.Records() {
		messages = append(messages, record.Message)
	}
	return messages
}

func newDurableTestStore(t *testing.T) (*localconfig.Store, string) {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	writeConnections(t, path, "https://example.test", "token", time.Now().Add(time.Hour))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

func durableTestEvents(eventIDs ...string) []durableTestEvent {
	events := make([]durableTestEvent, 0, len(eventIDs))
	for index, eventID := range eventIDs {
		events = append(events, durableTestEvent{
			ID: eventID, OccurredAt: time.Unix(int64(42+index), 0).UTC(), Payload: testTriggerConfiguration{ChannelID: "SENTINEL-PAYLOAD-C123"},
		})
	}
	return events
}

func prepareDurableTestEvents(t *testing.T, target sdkgo.TriggerTarget[testTriggerConfiguration], events []durableTestEvent) {
	t.Helper()
	for _, event := range events {
		require.NoError(t, sdkgo.PrepareTriggerDelivery(context.Background(), target, event))
	}
}

func replayDurableTestTarget(ctx context.Context, t *testing.T, target sdkgo.TriggerTarget[testTriggerConfiguration]) error {
	t.Helper()
	replayer, ok := target.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	return replayer.ReplayTriggerDeliveries(ctx)
}

func unavailableDexError() error {
	return &dex.ServiceError{Op: "InvokeRPC", FlowID: "flow-1", Code: codes.Unavailable, Detail: "connection refused"}
}

func TestDurableTriggerTargetConsumesUndeliverableEventAndKeepsRetryable(t *testing.T) {
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-closed-thread", "Ev-dex-down")
	logs := testsupport.NewLogRecorder()
	target := newLoggingDurableTestTarget(t, store, logs.Logger(), func(_ context.Context, event durableTestEvent) error {
		if event.ID == "Ev-closed-thread" {
			return sdkgo.MarkTriggerUndeliverable(&dex.FlowNotActiveError{ServiceError: &dex.ServiceError{
				Op: "InvokeRPC", FlowID: "flow-1", Code: codes.NotFound, Detail: "workflow execution already completed",
			}})
		}
		return unavailableDexError()
	})
	prepareDurableTestEvents(t, target, events)

	require.NoError(t, target.HandleTrigger(context.Background(), events[0]))
	require.Equal(t, []string{"Ev-dex-down"}, pendingTriggerEventIDs(t, directory))
	records := logs.Records()
	require.Len(t, records, 1)
	require.Equal(t, slog.LevelWarn, records[0].Level)
	require.Equal(t, "trigger event skipped: undeliverable", records[0].Message)
	require.Equal(t, inboxLogAttrs("event_id", "Ev-closed-thread", "flow_id", "flow-1", "error",
		`Trigger event is undeliverable: dex: InvokeRPC flow "flow-1": NotFound: workflow execution already completed`,
	), records[0].Attrs)

	err := target.HandleTrigger(context.Background(), events[1])
	var serviceError *dex.ServiceError
	require.ErrorAs(t, err, &serviceError)
	require.Equal(t, codes.Unavailable, serviceError.Code)
	require.False(t, sdkgo.IsTriggerUndeliverable(err))
	require.Equal(t, []string{"Ev-dex-down"}, pendingTriggerEventIDs(t, directory))
	require.Len(t, logs.Records(), 1, "the inbox returns a retryable error for its caller to log")
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// TestDurableTriggerSkipAfterLiveRetryIsNotLoggedAsDelivered covers a live delivery that fails once and is
// then undeliverable: DeliverTrigger logs the retry, the inbox logs the skip, and nothing logs a delivery.
func TestDurableTriggerSkipAfterLiveRetryIsNotLoggedAsDelivered(t *testing.T) {
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-late")
	logs := testsupport.NewLogRecorder()
	var attempts atomic.Int32
	target := newLoggingDurableTestTarget(t, store, logs.Logger(), func(context.Context, durableTestEvent) error {
		if attempts.Add(1) == 1 {
			return unavailableDexError()
		}
		return sdkgo.MarkTriggerUndeliverable(errors.New("workflow execution already completed"))
	})
	prepareDurableTestEvents(t, target, events)
	// The caller's event-scoped attributes reach the inbox's skip record, so one thread ID finds every record.
	ctx := sdkgo.ContextWithTriggerLogAttrs(context.Background(), slog.String("thread_ts", "1.0"))
	require.NoError(t, sdkgo.DeliverTrigger(ctx, target, events[0], sdkgo.WithTriggerLogger(logs.Logger())))
	require.Equal(t, int32(2), attempts.Load())
	require.Empty(t, pendingTriggerEventIDs(t, directory))
	require.Equal(t, []string{"trigger delivery failed; retrying", "trigger event skipped: undeliverable"}, logMessages(logs))
	require.Equal(t, map[string]string{
		"thread_ts": "1.0", "event_id": "Ev-late", "attempt": "1", "delay": "250ms", "flow_id": "flow-1", "error": unavailableDexError().Error(),
	}, logs.Find("trigger delivery failed; retrying")[0].Attrs)
	require.Equal(t, inboxLogAttrs("thread_ts", "1.0", "event_id", "Ev-late", "error", "Trigger event is undeliverable: workflow execution already completed"),
		logs.Find("trigger event skipped: undeliverable")[0].Attrs)
}

// TestDurableTriggerReportsItsSkipToTheEnclosingAttempt covers a poller that retries on its own schedule:
// TriggerAttempt tells it that the inbox consumed and logged the event, so it does not report a delivery.
func TestDurableTriggerReportsItsSkipToTheEnclosingAttempt(t *testing.T) {
	store, _ := newDurableTestStore(t)
	events := durableTestEvents("Ev-closed", "Ev-open")
	logs := testsupport.NewLogRecorder()
	target := newLoggingDurableTestTarget(t, store, logs.Logger(), func(_ context.Context, event durableTestEvent) error {
		if event.ID == "Ev-closed" {
			return sdkgo.MarkTriggerUndeliverable(errors.New("workflow execution already completed"))
		}
		return nil
	})
	prepareDurableTestEvents(t, target, events)

	attemptCtx, skipped := sdkgo.TriggerAttempt(context.Background())
	require.NoError(t, target.HandleTrigger(attemptCtx, events[0]))
	require.True(t, skipped(), "the inbox consumed the event as undeliverable and logged the skip")
	attemptCtx, skipped = sdkgo.TriggerAttempt(context.Background())
	require.NoError(t, target.HandleTrigger(attemptCtx, events[1]))
	require.False(t, skipped())
	require.Equal(t, []string{"trigger event skipped: undeliverable"}, logMessages(logs))
}

// TestDurableTriggerRecordsReportTheInboxAsTheirSource checks that a handler with AddSource sees the inbox,
// not the shared logging helper, as the source of a record.
func TestDurableTriggerRecordsReportTheInboxAsTheirSource(t *testing.T) {
	store, _ := newDurableTestStore(t)
	events := durableTestEvents("Ev-closed")
	var output strings.Builder
	logger := slog.New(slog.NewTextHandler(&output, &slog.HandlerOptions{AddSource: true}))
	target := newLoggingDurableTestTarget(t, store, logger, func(context.Context, durableTestEvent) error {
		return sdkgo.MarkTriggerUndeliverable(errors.New("workflow execution already completed"))
	})
	prepareDurableTestEvents(t, target, events)
	require.NoError(t, target.HandleTrigger(context.Background(), events[0]))
	require.Contains(t, output.String(), "localconfig/localconfig.go:")
	require.NotContains(t, output.String(), "triggerlog")
}

// TestDurableTriggerLogsInboxFailures covers the ERROR records for inbox read and write failures.
func TestDurableTriggerLogsInboxFailures(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permissions this test relies on")
	}
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-first", "Ev-second")
	logs := testsupport.NewLogRecorder()
	target := newLoggingDurableTestTarget(t, store, logs.Logger(), func(context.Context, durableTestEvent) error { return nil })

	// A read-only directory lets the inbox be read but not created, as a full disk would.
	require.NoError(t, os.Chmod(directory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	err := sdkgo.PrepareTriggerDelivery(context.Background(), target, events[0])
	require.ErrorContains(t, err, "Trigger inbox update")
	written := logs.Find("trigger inbox write failed")
	require.Len(t, written, 1)
	require.Equal(t, slog.LevelError, written[0].Level)
	require.Equal(t, inboxLogAttrs("event_id", "Ev-first", "error", err.Error()), written[0].Attrs)
	require.NoError(t, os.Chmod(directory, 0o700))

	// An inbox with the wrong permissions cannot be read.
	prepareDurableTestEvents(t, target, events[:1])
	paths, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	require.Len(t, paths, 1)
	require.NoError(t, os.Chmod(paths[0], 0o644))
	err = sdkgo.PrepareTriggerDelivery(context.Background(), target, events[1])
	require.ErrorContains(t, err, "Trigger inbox must be a regular 0600 file")
	err = replayDurableTestTarget(context.Background(), t, target)
	require.ErrorContains(t, err, "Trigger inbox must be a regular 0600 file")
	read := logs.Find("trigger inbox read failed")
	require.Len(t, read, 2)
	require.Equal(t, slog.LevelError, read[0].Level)
	require.Equal(t, inboxLogAttrs("event_id", "Ev-second", "error", "Trigger inbox must be a regular 0600 file"), read[0].Attrs)
	require.Equal(t, inboxLogAttrs("error", "Trigger inbox must be a regular 0600 file"), read[1].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")
}

func TestDurableTriggerReplayConsumesUndeliverableAndRetriesTransient(t *testing.T) {
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-poison", "Ev-flaky", "Ev-good")
	prepareDurableTestEvents(t, newDurableTestTarget(t, store, func(context.Context, durableTestEvent) error { return nil }), events)

	var calls []string
	flakyFailures := 0
	logs := testsupport.NewLogRecorder()
	restartedTarget := newLoggingDurableTestTarget(t, store, logs.Logger(), func(_ context.Context, event durableTestEvent) error {
		calls = append(calls, event.ID)
		switch event.ID {
		case "Ev-poison":
			return sdkgo.MarkTriggerUndeliverable(errors.New("reply belongs to a completed Flow"))
		case "Ev-flaky":
			if flakyFailures < 2 {
				flakyFailures++
				return unavailableDexError()
			}
		}
		return nil
	})
	require.NoError(t, replayDurableTestTarget(context.Background(), t, restartedTarget))
	require.Equal(t, []string{"Ev-poison", "Ev-flaky", "Ev-flaky", "Ev-flaky", "Ev-good"}, calls)
	require.Empty(t, pendingTriggerEventIDs(t, directory))

	require.Equal(t, []string{
		"replaying pending trigger events",
		"trigger event skipped: undeliverable",
		"trigger delivery failed; retrying",
		"trigger delivery failed; retrying",
		"trigger delivered after retry",
		"finished replaying pending trigger events",
	}, logMessages(logs))
	records := logs.Records()
	require.Equal(t, slog.LevelInfo, records[0].Level)
	require.Equal(t, inboxLogAttrs("count", "3"), records[0].Attrs)
	require.Equal(t, slog.LevelWarn, records[1].Level)
	require.Equal(t, inboxLogAttrs("event_id", "Ev-poison", "error", "Trigger event is undeliverable: reply belongs to a completed Flow"), records[1].Attrs)
	for index, delay := range []string{"250ms", "500ms"} {
		require.Equal(t, slog.LevelWarn, records[2+index].Level)
		require.Equal(t, inboxLogAttrs("event_id", "Ev-flaky", "attempt", []string{"1", "2"}[index], "delay", delay,
			"flow_id", "flow-1", "error", unavailableDexError().Error()), records[2+index].Attrs)
	}
	require.Equal(t, slog.LevelInfo, records[4].Level)
	require.Equal(t, inboxLogAttrs("event_id", "Ev-flaky", "attempts", "3"), records[4].Attrs)
	require.Equal(t, slog.LevelInfo, records[5].Level)
	require.Equal(t, inboxLogAttrs("delivered", "2", "skipped", "1", "remaining", "0"), records[5].Attrs)
	require.NotContains(t, logs.Text(), "SENTINEL")

	calls = nil
	require.NoError(t, replayDurableTestTarget(context.Background(), t, restartedTarget))
	require.Empty(t, calls)
	require.Len(t, logs.Records(), 6, "a replay that finds no pending events logs nothing")
}

func TestDurableTriggerReplayKeepsEventWhenContextEnds(t *testing.T) {
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-pending")
	prepareDurableTestEvents(t, newDurableTestTarget(t, store, func(context.Context, durableTestEvent) error { return nil }), events)

	logs := testsupport.NewLogRecorder()
	failingTarget := newLoggingDurableTestTarget(t, store, logs.Logger(), func(context.Context, durableTestEvent) error { return unavailableDexError() })
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := replayDurableTestTarget(ctx, t, failingTarget)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, []string{"Ev-pending"}, pendingTriggerEventIDs(t, directory))
	finished := logs.Find("finished replaying pending trigger events")
	require.Len(t, finished, 1)
	require.Equal(t, slog.LevelInfo, finished[0].Level)
	require.Equal(t, inboxLogAttrs("delivered", "0", "skipped", "0", "remaining", "1", "error", context.DeadlineExceeded.Error()), finished[0].Attrs)

	var delivered []string
	recoveredTarget := newDurableTestTarget(t, store, func(_ context.Context, event durableTestEvent) error {
		delivered = append(delivered, event.ID)
		return nil
	})
	require.NoError(t, replayDurableTestTarget(context.Background(), t, recoveredTarget))
	require.Equal(t, []string{"Ev-pending"}, delivered)
	require.Empty(t, pendingTriggerEventIDs(t, directory))
}

func TestDurableTriggerReplayReleasesInboxDuringBackoff(t *testing.T) {
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-backing-off", "Ev-new")
	prepareDurableTestEvents(t, newDurableTestTarget(t, store, func(context.Context, durableTestEvent) error { return nil }), events[:1])

	firstAttempt := make(chan struct{})
	var attempts atomic.Int32
	target := newDurableTestTarget(t, store, func(context.Context, durableTestEvent) error {
		if attempts.Add(1) == 1 {
			close(firstAttempt)
		}
		return unavailableDexError()
	})
	replayer, ok := target.(sdkgo.TriggerDeliveryReplayer)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	replayResult := make(chan error, 1)
	go func() { replayResult <- replayer.ReplayTriggerDeliveries(ctx) }()
	select {
	case <-firstAttempt:
	case <-time.After(time.Second):
		t.Fatal("replay did not attempt the pending event")
	}

	prepared := make(chan error, 1)
	go func() { prepared <- sdkgo.PrepareTriggerDelivery(context.Background(), target, events[1]) }()
	select {
	case err := <-prepared:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("PrepareTriggerDelivery waited for replay backoff")
	}
	require.Equal(t, []string{"Ev-backing-off", "Ev-new"}, pendingTriggerEventIDs(t, directory))

	cancel()
	select {
	case err := <-replayResult:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("replay did not stop after cancellation")
	}
	require.Equal(t, []string{"Ev-backing-off", "Ev-new"}, pendingTriggerEventIDs(t, directory))
}

func TestDurableTriggerRetriesOnlyTheRemovalAfterAFailedInboxWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the directory permissions this test relies on")
	}
	store, directory := newDurableTestStore(t)
	events := durableTestEvents("Ev-applied")
	var calls atomic.Int32
	logs := testsupport.NewLogRecorder()
	target := newLoggingDurableTestTarget(t, store, logs.Logger(), func(context.Context, durableTestEvent) error {
		calls.Add(1)
		return nil
	})
	prepareDurableTestEvents(t, target, events)

	// A read-only directory lets the inbox be read but not replaced, as a full disk would.
	require.NoError(t, os.Chmod(directory, 0o500))
	t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
	err := target.HandleTrigger(context.Background(), events[0])
	require.ErrorContains(t, err, "Trigger inbox update")
	require.Equal(t, int32(1), calls.Load())
	err = target.HandleTrigger(context.Background(), events[0])
	require.ErrorContains(t, err, "Trigger inbox update")
	require.Equal(t, int32(1), calls.Load(), "a failed removal must not invoke the target again")
	removals := logs.Find("trigger inbox remove failed")
	require.Len(t, removals, 2)
	for _, record := range removals {
		require.Equal(t, slog.LevelError, record.Level)
		require.Equal(t, "Ev-applied", record.Attrs["event_id"])
		require.Contains(t, record.Attrs["error"], "Trigger inbox update")
	}

	require.NoError(t, os.Chmod(directory, 0o700))
	require.NoError(t, replayDurableTestTarget(context.Background(), t, target))
	require.Equal(t, int32(1), calls.Load())
	require.Empty(t, pendingTriggerEventIDs(t, directory))

	// After the removal succeeds, an event with the same ID is delivered normally again.
	prepareDurableTestEvents(t, target, events)
	require.NoError(t, target.HandleTrigger(context.Background(), events[0]))
	require.Equal(t, int32(2), calls.Load())
	require.Empty(t, pendingTriggerEventIDs(t, directory))
}

// pendingTriggerEventIDs lists the event IDs persisted in every Trigger inbox in directory.
func pendingTriggerEventIDs(t *testing.T, directory string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	eventIDs := []string{}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var inbox struct {
			Events []struct {
				EventID string `json:"eventId"`
			} `json:"events"`
		}
		require.NoError(t, json.Unmarshal(contents, &inbox))
		for _, event := range inbox.Events {
			eventIDs = append(eventIDs, event.EventID)
		}
	}
	return eventIDs
}

func decodeTestCredentials(contents json.RawMessage) (testCredentials, error) {
	var raw struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(contents, &raw); err != nil {
		return testCredentials{}, err
	}
	return testCredentials{AccessToken: sdkgo.NewSecretString(raw.AccessToken)}, nil
}

func writeConnections(t *testing.T, path string, endpoint string, token string, expiresAt time.Time) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": "gmail", "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
			"moduleVersion": "v0.1.1", "provider": "google", "connectionName": "sender",
			"configuration": map[string]any{"endpoint": endpoint}, "credentials": map[string]any{"access_token": token},
			"credentialExpiresAt": expiresAt.UTC().Format(time.RFC3339),
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

func writeUseConfigurations(t *testing.T, directory string, reference sdkgo.ConnectorConfigurationRef, configuration map[string]any) {
	t.Helper()
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.UseConfigurationsSchemaVersion,
		"operationConfigurations": []any{map[string]any{
			"connectorId": reference.ConnectorID, "connectionName": reference.ConnectionName,
			"operationId": reference.OperationID, "flowType": reference.FlowType, "stepType": reference.StepType,
			"configuration": configuration,
		}},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, localconfig.UseConfigurationsFileName), contents, 0o600))
}
