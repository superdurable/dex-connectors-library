// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

// Sentinels stand in for secrets and message text. No log record may contain them.
const (
	sentinelBotToken  = "SENTINEL-BOT-TOKEN"
	sentinelUserToken = "SENTINEL-USER-TOKEN"
	sentinelAppToken  = "SENTINEL-APP-TOKEN"
	sentinelText      = "SENTINEL-MESSAGE-TEXT"
)

// requireNoSentinel fails when any captured record contains a token, ticket, or message text sentinel.
func requireNoSentinel(t *testing.T, logs *testlog.LogRecorder) {
	t.Helper()
	require.NotContains(t, logs.Text(), "SENTINEL")
}

func TestChannelThreadTriggerMatchesChannelPosterAndText(t *testing.T) {
	source := &messageTriggerSource{
		channelID: "C123", matcher: MessageMatcher{MessageContains: "request approval", PosterUserIDs: []string{"U1"}},
	}
	envelope := messageEnvelope(t, "Ev1", messageEvent{Type: "message", Channel: "C123", User: "U1", Text: "Please REQUEST APPROVAL for invoice 42", Timestamp: "1.0"})
	matched, event, err := source.decodeEvent(envelope)
	require.NoError(t, err)
	require.True(t, matched)
	require.Equal(t, "Ev1", event.ID)
	require.Equal(t, "1.0", event.Payload.ThreadTimestamp)
}

func TestThreadReplyTriggerIgnoresRootBotAndDisallowedPoster(t *testing.T) {
	source := &messageTriggerSource{
		channelID: "C123", requiresThread: true,
		matcher: MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}},
	}
	cases := []messageEvent{
		{Type: "message", Channel: "C123", User: "U2", Text: "approve", Timestamp: "1.0"},
		{Type: "message", Channel: "C123", User: "U2", BotID: "B1", Text: "approve", Timestamp: "2.0", ThreadTS: "1.0"},
		{Type: "message", Channel: "C123", User: "U3", Text: "approve", Timestamp: "2.0", ThreadTS: "1.0"},
	}
	for index, message := range cases {
		matched, _, err := source.decodeEvent(messageEnvelope(t, "Ev", message))
		require.NoError(t, err)
		require.False(t, matched, "case %d", index)
	}
	matched, _, err := source.decodeEvent(messageEnvelope(t, "Ev4", messageEvent{Type: "message", Channel: "C123", User: "U2", Text: "disapprove", Timestamp: "2.0", ThreadTS: "1.0"}))
	require.NoError(t, err)
	require.True(t, matched)
}

func TestThreadReplyConfigurationRequiresApprover(t *testing.T) {
	err := (ThreadReplyCreatedTriggerConfiguration{ChannelID: "C123"}).Validate()
	require.ErrorContains(t, err, "requires at least one")
}

func TestSocketModeReconnectsDeliversAndAcknowledgesMatchedEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer app-token", request.Header.Get("Authorization"))
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	defer server.Close()
	first := &fakeSocketConnection{readErr: errors.New("connection lost")}
	second := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{messageEnvelope(t, "Ev2", messageEvent{
		Type: "message", Channel: "C123", User: "U1", Text: "request approval", Timestamp: "1.0",
	})}}
	var dialMu sync.Mutex
	dialCount := 0
	dialer := func(context.Context, string) (socketConnection, error) {
		dialMu.Lock()
		defer dialMu.Unlock()
		dialCount++
		if dialCount == 1 {
			return first, nil
		}
		return second, nil
	}
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token")},
	}, func(options *clientOptions) { options.socketDialer = dialer })
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	delivered := make(chan sdkgo.TriggerEvent[MessageEvent], 1)
	runner := NewChannelThreadCreatedTrigger(ChannelThreadCreatedTriggerConfig{
		Connection: connection, ConnectionName: "workspace", BindingName: "approval-start",
		Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123", ThreadTriggerMatcher: MessageMatcher{MessageContains: "request approval"}},
		Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
			delivered <- event
			cancel()
			return nil
		}),
	})
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	select {
	case event := <-delivered:
		require.Equal(t, "Ev2", event.ID)
	case <-time.After(3 * time.Second):
		t.Fatal("matched Slack event was not delivered")
	}
	select {
	case acknowledgement := <-second.acknowledgements:
		require.Equal(t, "envelope-Ev2", acknowledgement["envelope_id"])
	case <-time.After(time.Second):
		t.Fatal("Slack envelope was not acknowledged")
	}
	select {
	case runErr := <-runFinished:
		require.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("trigger runner did not stop")
	}
}

func TestSocketModeAcknowledgesBeforeTargetCompletes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	defer server.Close()
	connection := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{messageEnvelope(t, "Ev3", messageEvent{
		Type: "message", Channel: "C123", User: "U1", Text: "request approval", Timestamp: "1.0",
	})}}
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"), AppToken: sdkgo.NewSecretString("app-token")},
	}, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) { return connection, nil }
	})
	require.NoError(t, err)
	configuredConnection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	targetStarted := make(chan struct{})
	releaseTarget := make(chan struct{})
	runner := NewChannelThreadCreatedTrigger(ChannelThreadCreatedTriggerConfig{
		Connection: configuredConnection, BindingName: "approval-start",
		Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
		Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
			close(targetStarted)
			<-releaseTarget
			return nil
		}),
	})
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	select {
	case <-targetStarted:
	case <-time.After(time.Second):
		t.Fatal("trigger target did not start")
	}
	select {
	case acknowledgement := <-connection.acknowledgements:
		require.Equal(t, "envelope-Ev3", acknowledgement["envelope_id"])
	case <-time.After(time.Second):
		t.Fatal("Slack envelope was not acknowledged before target completion")
	}
	close(releaseTarget)
	cancel()
	select {
	case <-runFinished:
	case <-time.After(time.Second):
		t.Fatal("trigger runner did not stop")
	}
}

func TestMessageTriggerRunnerSharesSocketAcrossRootAndReplyRoutes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	defer server.Close()
	socket := &fakeSocketConnection{
		acknowledgements: make(chan map[string]string, 2),
		envelopes: []socketEnvelope{
			messageEnvelope(t, "EvRoot", messageEvent{
				Type: "message", Channel: "C123", User: "U1", Text: "request approval", Timestamp: "1.0",
			}),
			messageEnvelope(t, "EvReply", messageEvent{
				Type: "message", Channel: "C123", User: "U2", Text: "approve", Timestamp: "2.0", ThreadTS: "1.0",
			}),
		},
	}
	dialCount := 0
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {
			BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"),
			AppToken: sdkgo.NewSecretString("app-token"),
		},
	}, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) {
			dialCount++
			return socket, nil
		}
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rootEvents := make(chan sdkgo.TriggerEvent[MessageEvent], 1)
	replyEvents := make(chan sdkgo.TriggerEvent[MessageEvent], 1)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start",
			Configuration: ChannelThreadCreatedTriggerConfiguration{
				ChannelID: "C123", ThreadTriggerMatcher: MessageMatcher{MessageContains: "request approval"},
			},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				rootEvents <- event
				return nil
			}),
		}},
		ThreadReplyCreatedRoutes: []ThreadReplyCreatedTriggerRoute{{
			BindingName: "approval-reply",
			Configuration: ThreadReplyCreatedTriggerConfiguration{
				ChannelID: "C123", ThreadReplyMatcher: MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}},
			},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				replyEvents <- event
				cancel()
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	require.Equal(t, "EvRoot", receiveTriggerEvent(t, rootEvents).ID)
	require.Equal(t, "EvReply", receiveTriggerEvent(t, replyEvents).ID)
	for _, eventID := range []string{"EvRoot", "EvReply"} {
		select {
		case acknowledgement := <-socket.acknowledgements:
			require.Equal(t, "envelope-"+eventID, acknowledgement["envelope_id"])
		case <-time.After(time.Second):
			t.Fatalf("Slack envelope %s was not acknowledged", eventID)
		}
	}
	select {
	case runErr := <-runFinished:
		require.ErrorIs(t, runErr, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("message Trigger runner did not stop")
	}
	require.Equal(t, 1, dialCount)
}

func TestRunnerRetriesRetryableTargetErrorAfterAcknowledgement(t *testing.T) {
	socket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{
		messageEnvelope(t, "EvRetry", messageEvent{Type: "message", Channel: "C123", User: "U1", Text: sentinelText + " request approval", Timestamp: "1.0"}),
	}}
	logs := testlog.NewLogRecorder()
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return socket, nil },
		WithLogger(logs.Logger()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var attempts atomic.Int32
	var acknowledgedBeforeFirstAttempt atomic.Bool
	delivered := make(chan sdkgo.TriggerEvent[MessageEvent], 1)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				if attempts.Add(1) == 1 {
					acknowledgedBeforeFirstAttempt.Store(len(socket.acknowledgements) == 1)
					return errors.New("temporary Dex failure")
				}
				delivered <- event
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	select {
	case event := <-delivered:
		require.Equal(t, "EvRetry", event.ID)
	case <-time.After(2 * time.Second):
		t.Fatal("retryable Slack Trigger failure was not retried")
	}
	require.Equal(t, int32(2), attempts.Load())
	require.True(t, acknowledgedBeforeFirstAttempt.Load())
	require.Equal(t, "envelope-EvRetry", receiveAcknowledgement(t, socket.acknowledgements))
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)

	route := map[string]string{
		"connector": "slack", "connection": "workspace", "trigger": "channelThreadCreated", "binding": "approval-start",
		"channel": "C123", "thread_ts": "1.0", "event_id": "EvRetry",
	}
	retries := logs.Find("trigger delivery failed; retrying", nil)
	require.Len(t, retries, 1)
	require.Equal(t, slog.LevelWarn, retries[0].Level)
	require.Equal(t, withAttrs(route, "attempt", "1", "delay", "250ms", "error", "temporary Dex failure"), retries[0].Attrs)
	recovered := logs.Find("trigger delivered after retry", nil)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, withAttrs(route, "attempts", "2"), recovered[0].Attrs)
	connected := logs.Find("slack socket mode connected", nil)
	require.Len(t, connected, 1)
	require.Equal(t, slog.LevelInfo, connected[0].Level)
	require.Equal(t, map[string]string{"connector": "slack", "connection": "workspace", "reconnect": "false"}, connected[0].Attrs)
	requireNoSentinel(t, logs)
}

// withAttrs copies attrs and adds the key-value pairs in extra.
func withAttrs(attrs map[string]string, extra ...string) map[string]string {
	combined := make(map[string]string, len(attrs)+len(extra)/2)
	for key, value := range attrs {
		combined[key] = value
	}
	for index := 0; index+1 < len(extra); index += 2 {
		combined[extra[index]] = extra[index+1]
	}
	return combined
}

func TestSharedRunnerConsumesUndeliverableReplyAndKeepsReading(t *testing.T) {
	socket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 2), envelopes: []socketEnvelope{
		messageEnvelope(t, "EvOrphanReply", messageEvent{Type: "message", Channel: "C123", User: "U2", Text: sentinelText + " approve", Timestamp: "9.0", ThreadTS: "8.0"}),
		messageEnvelope(t, "EvRoot", messageEvent{Type: "message", Channel: "C123", User: "U1", Text: sentinelText + " request approval", Timestamp: "10.0"}),
	}}
	logs := testlog.NewLogRecorder()
	connection := newFakeSocketModeConnection(t, func(context.Context, string) (socketConnection, error) { return socket, nil },
		WithLogger(logs.Logger()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var replyAttempts, rootCalls atomic.Int32
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
				rootCalls.Add(1)
				return nil
			}),
		}},
		ThreadReplyCreatedRoutes: []ThreadReplyCreatedTriggerRoute{{
			BindingName: "approval-reply",
			Configuration: ThreadReplyCreatedTriggerConfiguration{
				ChannelID: "C123", ThreadReplyMatcher: MessageMatcher{PosterUserIDs: []string{"U2"}},
			},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
				replyAttempts.Add(1)
				return sdkgo.MarkTriggerUndeliverable(flowNotActiveError())
			}),
		}},
	})
	require.NoError(t, err)
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	require.Equal(t, "envelope-EvOrphanReply", receiveAcknowledgement(t, socket.acknowledgements))
	require.Equal(t, "envelope-EvRoot", receiveAcknowledgement(t, socket.acknowledgements))
	require.Eventually(t, func() bool { return rootCalls.Load() == 1 }, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, int32(1), replyAttempts.Load())
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)

	skipped := logs.Find("trigger event skipped: undeliverable", nil)
	require.Len(t, skipped, 1)
	require.Equal(t, slog.LevelWarn, skipped[0].Level)
	require.Equal(t, map[string]string{
		"connector": "slack", "connection": "workspace", "trigger": "threadReplyCreated", "binding": "approval-reply",
		"channel": "C123", "thread_ts": "8.0", "event_id": "EvOrphanReply", "flow_id": "slack-thread",
		"error": "Trigger event is undeliverable: " + flowNotActiveError().Error(),
	}, skipped[0].Attrs)
	// Each route logs why it ignores the other route's message.
	ignored := logs.Find("trigger event ignored", nil)
	require.Len(t, ignored, 2)
	require.Equal(t, slog.LevelDebug, ignored[0].Level)
	require.Equal(t, map[string]string{
		"connector": "slack", "connection": "workspace", "trigger": "channelThreadCreated", "binding": "approval-start",
		"event_id": "EvOrphanReply", "channel": "C123", "reason": "not_a_root",
	}, ignored[0].Attrs)
	require.Equal(t, map[string]string{
		"connector": "slack", "connection": "workspace", "trigger": "threadReplyCreated", "binding": "approval-reply",
		"event_id": "EvRoot", "channel": "C123", "reason": "not_a_reply",
	}, ignored[1].Attrs)
	require.Empty(t, logs.Find("trigger delivered after retry", nil))
	requireNoSentinel(t, logs)
}

// newLocalSlackStore writes a local connection file with one root and one reply binding for channel C1.
func newLocalSlackStore(t *testing.T) (*localconfig.Store, string) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"url":"ws://socket.test"}`))
	}))
	t.Cleanup(server.Close)
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/slack",
			"moduleVersion": "v0.7.0", "provider": "slack", "connectionName": "workspace",
			"configuration": map[string]any{"endpoint": server.URL},
			"credentials":   map[string]any{"bot_token": sentinelBotToken, "user_token": sentinelUserToken, "app_token": sentinelAppToken},
		}},
		"triggerBindings": []any{
			map[string]any{"connectorId": ConnectorID, "connectionName": "workspace", "triggerName": "channelThreadCreated", "bindingName": "approval-start",
				"configuration": map[string]any{"channelId": "C1", "threadTriggerMatcher": map[string]any{"messageContains": "request approval"}}},
			map[string]any{"connectorId": ConnectorID, "connectionName": "workspace", "triggerName": "threadReplyCreated", "bindingName": "approval-reply",
				"configuration": map[string]any{"channelId": "C1", "threadReplyMatcher": map[string]any{"messageContains": "approve", "posterUserIds": []string{"U2"}}}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

func TestLocalRunnerReplaysPastUndeliverableEventAfterRestart(t *testing.T) {
	store, directory := newLocalSlackStore(t)
	logs := testlog.NewLogRecorder()
	var rootCalls atomic.Int32
	rootEvents := make(chan string, 4)
	config := LocalMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []LocalChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start",
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				rootCalls.Add(1)
				rootEvents <- event.ID
				return nil
			}),
		}},
		ThreadReplyCreatedRoutes: []LocalThreadReplyCreatedTriggerRoute{{
			BindingName: "approval-reply",
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error {
				return sdkgo.MarkTriggerUndeliverable(flowNotActiveError())
			}),
		}},
	}
	runLocal := func(socket *fakeSocketConnection) (*atomic.Int32, context.CancelFunc, chan error) {
		var dials atomic.Int32
		runner, err := NewLocalMessageTriggerRunner(store, "workspace", config, func(options *clientOptions) {
			options.socketDialer = func(context.Context, string) (socketConnection, error) {
				dials.Add(1)
				return socket, nil
			}
		}, WithLogger(logs.Logger()))
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		runFinished := make(chan error, 1)
		go func() { runFinished <- runner.Run(ctx) }()
		return &dials, cancel, runFinished
	}

	firstSocket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 2), envelopes: []socketEnvelope{
		messageEnvelope(t, "EvOrphanReply", messageEvent{Type: "message", Channel: "C1", User: "U2", Text: sentinelText + " I approve", Timestamp: "5.0", ThreadTS: "4.0"}),
		messageEnvelope(t, "EvRoot", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: sentinelText + " request approval", Timestamp: "6.0"}),
	}}
	_, cancelFirst, firstFinished := runLocal(firstSocket)
	defer cancelFirst()
	require.Equal(t, "envelope-EvOrphanReply", receiveAcknowledgement(t, firstSocket.acknowledgements))
	require.Equal(t, "envelope-EvRoot", receiveAcknowledgement(t, firstSocket.acknowledgements))
	require.Equal(t, "EvRoot", receiveEventID(t, rootEvents))
	cancelFirst()
	require.ErrorIs(t, receiveRunResult(t, firstFinished), context.Canceled)

	// Persist an undeliverable reply as if the process crashed after acknowledging it.
	replyInbox, err := localconfig.NewDurableTriggerTarget(store, ConnectorID, "workspace", "threadReplyCreated", "approval-reply",
		sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil }))
	require.NoError(t, err)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(context.Background(), replyInbox, sdkgo.TriggerEvent[MessageEvent]{
		ID: "EvLateReply", Payload: MessageEvent{TeamID: "T1", ChannelID: "C1", Timestamp: "4.2", ThreadTimestamp: "4.0", UserID: "U2", Text: sentinelText + " approve"},
	}))

	secondSocket := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{
		messageEnvelope(t, "EvRoot2", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "request approval", Timestamp: "7.0"}),
	}}
	dials, cancelSecond, secondFinished := runLocal(secondSocket)
	defer cancelSecond()
	require.Equal(t, "envelope-EvRoot2", receiveAcknowledgement(t, secondSocket.acknowledgements))
	require.Equal(t, "EvRoot2", receiveEventID(t, rootEvents))
	require.GreaterOrEqual(t, dials.Load(), int32(1))
	require.Equal(t, int32(2), rootCalls.Load())
	select {
	case err := <-secondFinished:
		t.Fatalf("restarted Slack Trigger runner returned before cancellation: %v", err)
	default:
	}
	cancelSecond()
	require.ErrorIs(t, receiveRunResult(t, secondFinished), context.Canceled)
	pending, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	for _, inboxPath := range pending {
		inbox, err := os.ReadFile(inboxPath)
		require.NoError(t, err)
		require.Contains(t, string(inbox), `"events":[]`)
	}

	// The durable inbox logs each skip once, with the binding identity and the closed Flow, live and on
	// replay. A live delivery also names the channel and thread; the inbox replays events without them.
	replyBinding := map[string]string{
		"connector": "slack", "connection": "workspace", "trigger": "threadReplyCreated", "binding": "approval-reply",
	}
	skippedError := "Trigger event is undeliverable: " + flowNotActiveError().Error()
	for eventID, expected := range map[string]map[string]string{
		"EvOrphanReply": withAttrs(replyBinding, "channel", "C1", "thread_ts", "4.0", "event_id", "EvOrphanReply",
			"flow_id", "slack-thread", "error", skippedError),
		"EvLateReply": withAttrs(replyBinding, "event_id", "EvLateReply", "flow_id", "slack-thread", "error", skippedError),
	} {
		skipped := logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": eventID})
		require.Len(t, skipped, 1, eventID)
		require.Equal(t, slog.LevelWarn, skipped[0].Level)
		require.Equal(t, expected, skipped[0].Attrs)
	}
	replaying := logs.Find("replaying pending trigger events", nil)
	require.Len(t, replaying, 1, "only the restart finds a pending event")
	require.Equal(t, slog.LevelInfo, replaying[0].Level)
	require.Equal(t, withAttrs(replyBinding, "count", "1"), replaying[0].Attrs)
	finished := logs.Find("finished replaying pending trigger events", nil)
	require.Len(t, finished, 1)
	require.Equal(t, withAttrs(replyBinding, "delivered", "0", "skipped", "1", "remaining", "0"), finished[0].Attrs)
	requireNoSentinel(t, logs)
}

// TestLocalRunnerDeliversPendingRootBeforeReplyAfterReconnect covers a root persisted on a connection that
// failed to acknowledge it. The reply read on the next connection must not reach Dex before that root.
func TestLocalRunnerDeliversPendingRootBeforeReplyAfterReconnect(t *testing.T) {
	store, directory := newLocalSlackStore(t)
	var started atomic.Bool
	var mutex sync.Mutex
	outcomes := []string{}
	record := func(outcome string) {
		mutex.Lock()
		defer mutex.Unlock()
		outcomes = append(outcomes, outcome)
	}
	snapshot := func() []string {
		mutex.Lock()
		defer mutex.Unlock()
		return append([]string(nil), outcomes...)
	}
	config := LocalMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []LocalChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start",
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				started.Store(true)
				record("root:" + event.ID)
				return nil
			}),
		}},
		ThreadReplyCreatedRoutes: []LocalThreadReplyCreatedTriggerRoute{{
			BindingName: "approval-reply",
			// Like NewDexRPCTriggerTarget, a reply whose Flow has not started is undeliverable.
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				if !started.Load() {
					record("reply-undeliverable:" + event.ID)
					return sdkgo.MarkTriggerUndeliverable(flowNotActiveError())
				}
				record("reply:" + event.ID)
				return nil
			}),
		}},
	}
	sockets := []*fakeSocketConnection{
		{writeErr: errors.New("broken pipe"), envelopes: []socketEnvelope{
			messageEnvelope(t, "EvRoot", messageEvent{Type: "message", Channel: "C1", User: "U1", Text: "request approval", Timestamp: "6.0"}),
		}},
		{acknowledgements: make(chan map[string]string, 1), envelopes: []socketEnvelope{
			messageEnvelope(t, "EvReply", messageEvent{Type: "message", Channel: "C1", User: "U2", Text: "approve", Timestamp: "6.5", ThreadTS: "6.0"}),
		}},
	}
	var dials atomic.Int32
	runner, err := NewLocalMessageTriggerRunner(store, "workspace", config, func(options *clientOptions) {
		options.socketDialer = func(context.Context, string) (socketConnection, error) {
			dial := int(dials.Add(1)) - 1
			if dial >= len(sockets) {
				return nil, errors.New("no more Slack sockets")
			}
			return sockets[dial], nil
		}
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()

	require.Equal(t, "envelope-EvReply", receiveAcknowledgementWithin(t, sockets[1].acknowledgements, 5*time.Second))
	require.Eventually(t, func() bool { return len(snapshot()) == 2 }, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"root:EvRoot", "reply:EvReply"}, snapshot())
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
	require.Empty(t, pendingSlackEventIDs(t, directory))
}

// pendingSlackEventIDs lists the event IDs persisted in every Trigger inbox in directory.
func pendingSlackEventIDs(t *testing.T, directory string) []string {
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

// shortenSocketModeTimings scales the reconnect schedule and the read deadline down for one test and
// restores them when the test ends. Tests that call it must stop their runner before they return.
func shortenSocketModeTimings(t *testing.T, reconnectDelay time.Duration, reconnectMaxDelay time.Duration, readTimeout time.Duration) {
	t.Helper()
	previousDelay, previousMaxDelay, previousReadTimeout := socketModeReconnectDelay, socketModeReconnectMaxDelay, socketModeReadTimeout
	socketModeReconnectDelay, socketModeReconnectMaxDelay, socketModeReadTimeout = reconnectDelay, reconnectMaxDelay, readTimeout
	t.Cleanup(func() {
		socketModeReconnectDelay, socketModeReconnectMaxDelay, socketModeReadTimeout = previousDelay, previousMaxDelay, previousReadTimeout
	})
}

// TestSocketModeReconnectLogsFailuresDelaysAndConnections covers the reconnect loop's records: dial
// failures whose error names the ticketed Socket Mode URL, with a delay that doubles up to its cap, a
// reconnect, an undecodable envelope, and a read failure after that connection, whose attempt count and
// delay start again.
func TestSocketModeReconnectLogsFailuresDelaysAndConnections(t *testing.T) {
	shortenSocketModeTimings(t, 10*time.Millisecond, 30*time.Millisecond, 45*time.Second)
	socketURL := "wss://wss-primary.slack.test/link/?ticket=SENTINEL-TICKET&app_id=A1"
	second := &fakeSocketConnection{acknowledgements: make(chan map[string]string, 2), readErr: errors.New("connection reset by peer"),
		envelopes: []socketEnvelope{
			{EnvelopeID: "envelope-undecodable", Type: "events_api", Payload: json.RawMessage(`{"event_id":42,"event":{"text":"SENTINEL-MESSAGE-TEXT"}}`)},
			messageEnvelope(t, "EvRoot", messageEvent{Type: "message", Channel: "C123", User: "U1", Text: sentinelText + " request approval", Timestamp: "1.0"}),
		}}
	var dials atomic.Int32
	dialer := func(ctx context.Context, dialed string) (socketConnection, error) {
		switch dials.Add(1) {
		case 1, 2, 3:
			// gorilla/websocket returns url.Parse errors unchanged, and they quote the whole URL.
			return nil, &url.Error{Op: "parse", URL: dialed, Err: errors.New("invalid port")}
		case 4:
			return second, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	logs := testlog.NewLogRecorder()
	connection := newFakeSocketModeConnectionAt(t, socketURL, dialer, WithLogger(logs.Logger()))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	delivered := make(chan string, 1)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				delivered <- event.ID
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	require.Equal(t, "EvRoot", receiveEventIDWithin(t, delivered, 3*time.Second))
	require.Eventually(t, func() bool { return dials.Load() == 5 }, 2*time.Second, 5*time.Millisecond)
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)

	connectionAttrs := map[string]string{"connector": "slack", "connection": "workspace"}
	failures := logs.Find("slack socket mode connection failed; reconnecting", nil)
	require.Len(t, failures, 4)
	// Consecutive failures double the delay up to the cap; a connection that received envelopes resets it.
	for index, expected := range [][2]string{{"1", "10ms"}, {"2", "20ms"}, {"3", "30ms"}} {
		require.Equal(t, slog.LevelWarn, failures[index].Level)
		require.Equal(t, withAttrs(connectionAttrs, "attempt", expected[0], "delay", expected[1],
			"error", "connect Slack Socket Mode: parse Socket Mode URL: invalid port"), failures[index].Attrs)
	}
	require.Equal(t, slog.LevelWarn, failures[3].Level)
	require.Equal(t, withAttrs(connectionAttrs, "attempt", "1", "delay", "10ms",
		"error", "read Slack Socket Mode envelope: connection reset by peer"), failures[3].Attrs)
	connected := logs.Find("slack socket mode connected", nil)
	require.Len(t, connected, 1)
	require.Equal(t, slog.LevelInfo, connected[0].Level)
	require.Equal(t, withAttrs(connectionAttrs, "reconnect", "true"), connected[0].Attrs)
	undecodable := logs.Find("trigger event skipped: undecodable", nil)
	require.Len(t, undecodable, 1)
	require.Equal(t, slog.LevelWarn, undecodable[0].Level)
	require.Equal(t, "envelope-undecodable", undecodable[0].Attrs["envelope_id"])
	require.Contains(t, undecodable[0].Attrs["error"], "cannot unmarshal number")
	require.Equal(t, []string{
		"slack socket mode connection failed; reconnecting",
		"slack socket mode connection failed; reconnecting",
		"slack socket mode connection failed; reconnecting",
		"slack socket mode connected",
		"trigger event skipped: undecodable",
		"slack socket mode connection failed; reconnecting",
	}, logs.Messages())
	requireNoSentinel(t, logs)
}

func TestSocketModeReconnectBackoffIsCappedExponential(t *testing.T) {
	require.Equal(t, time.Second, socketModeReconnectBackoff(1))
	require.Equal(t, 2*time.Second, socketModeReconnectBackoff(2))
	require.Equal(t, 16*time.Second, socketModeReconnectBackoff(5))
	require.Equal(t, 30*time.Second, socketModeReconnectBackoff(6))
	require.Equal(t, 30*time.Second, socketModeReconnectBackoff(1000))
}

// TestSocketModeKeepsQuietConnectionAliveAndReconnectsOnRequest runs the real gorilla/websocket dialer
// against a server that only pings for several read timeouts, then asks for a reconnect as Slack does
// when it refreshes a connection. Neither is a failure, so neither logs a WARN. The second connection
// keeps pinging after the runner is canceled, so Run returns only because the runner closes its socket.
func TestSocketModeKeepsQuietConnectionAliveAndReconnectsOnRequest(t *testing.T) {
	shortenSocketModeTimings(t, 10*time.Millisecond, 30*time.Millisecond, 400*time.Millisecond)
	var sockets atomic.Int32
	var pongs atomic.Int32
	quietFor := 1200 * time.Millisecond
	delivered := make(chan string, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apps.connections.open":
			_, _ = response.Write([]byte(`{"ok":true,"url":"ws://` + request.Host + `/socket?ticket=SENTINEL-TICKET"}`))
		case "/socket":
			socket, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				return
			}
			defer socket.Close()
			socket.SetPongHandler(func(string) error {
				pongs.Add(1)
				return nil
			})
			// Read in the background so the server processes the client's pongs, and notice when the client
			// closes the socket.
			clientClosed := make(chan struct{})
			go func() {
				defer close(clientClosed)
				for {
					if _, _, err := socket.ReadMessage(); err != nil {
						return
					}
				}
			}()
			// ping sends a ping every 50ms until the client closes the socket or, when until is not zero,
			// until that time. It reports whether the client is still connected.
			ping := func(until time.Time) bool {
				for until.IsZero() || time.Now().Before(until) {
					if err := socket.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(time.Second)); err != nil {
						return false
					}
					select {
					case <-clientClosed:
						return false
					case <-time.After(50 * time.Millisecond):
					}
				}
				return true
			}
			if err := socket.WriteJSON(map[string]any{"type": "hello", "num_connections": 1}); err != nil {
				return
			}
			if sockets.Add(1) == 1 {
				if !ping(time.Now().Add(quietFor)) {
					return
				}
				_ = socket.WriteJSON(map[string]any{"type": "disconnect", "reason": "refresh_requested", "debug_info": map[string]any{"host": "slack.test"}})
				<-clientClosed
				return
			}
			payload, _ := json.Marshal(eventsAPIPayload{EventID: "EvAfterRefresh", EventTime: 42, TeamID: "T1", Event: messageEvent{
				Type: "message", Channel: "C123", User: "U1", Text: sentinelText + " request approval", Timestamp: "1.0",
			}})
			_ = socket.WriteJSON(socketEnvelope{EnvelopeID: "envelope-after-refresh", Type: "events_api", Payload: payload})
			ping(time.Time{})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	logs := testlog.NewLogRecorder()
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {
			BotToken: sdkgo.NewSecretString(sentinelBotToken), UserToken: sdkgo.NewSecretString(sentinelUserToken),
			AppToken: sdkgo.NewSecretString(sentinelAppToken),
		},
	}, WithLogger(logs.Logger()))
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageEvent]) error {
				delivered <- event.ID
				return nil
			}),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	require.Equal(t, "EvAfterRefresh", receiveEventIDWithin(t, delivered, 5*time.Second))
	// Let the pinging second connection outlast one read timeout before canceling.
	time.Sleep(2 * socketModeReadTimeout)
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)

	require.GreaterOrEqual(t, pongs.Load(), int32(10), "the client answers every ping")
	require.Empty(t, logs.Find("slack socket mode connection failed; reconnecting", nil),
		"pings keep a quiet connection alive past three read timeouts")
	infoMessages := []string{}
	for _, record := range logs.Records() {
		if record.Level >= slog.LevelInfo {
			infoMessages = append(infoMessages, record.Message)
		}
	}
	require.Equal(t, []string{
		"slack socket mode connected",
		"slack socket mode disconnected; reconnecting",
		"slack socket mode connected",
	}, infoMessages)
	disconnected := logs.Find("slack socket mode disconnected; reconnecting", nil)
	require.Equal(t, slog.LevelInfo, disconnected[0].Level)
	require.Equal(t, map[string]string{
		"connector": "slack", "connection": "workspace", "reason": "refresh_requested", "delay": "10ms",
	}, disconnected[0].Attrs)
	require.Equal(t, "true", logs.Find("slack socket mode connected", nil)[1].Attrs["reconnect"])
	requireNoSentinel(t, logs)
}

// TestSocketModeReadTimeoutStillEndsASilentConnection keeps the deadline meaningful: a connection that
// receives neither envelopes nor pings fails and reconnects.
func TestSocketModeReadTimeoutStillEndsASilentConnection(t *testing.T) {
	shortenSocketModeTimings(t, 10*time.Millisecond, 30*time.Millisecond, 200*time.Millisecond)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apps.connections.open":
			_, _ = response.Write([]byte(`{"ok":true,"url":"ws://` + request.Host + `/socket"}`))
		case "/socket":
			socket, err := upgrader.Upgrade(response, request, nil)
			if err != nil {
				return
			}
			defer socket.Close()
			_ = socket.WriteJSON(map[string]any{"type": "hello", "num_connections": 1})
			// Stay silent until the client closes the socket.
			for {
				if _, _, err := socket.ReadMessage(); err != nil {
					return
				}
			}
		}
	}))
	defer server.Close()
	logs := testlog.NewLogRecorder()
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {BotToken: sdkgo.NewSecretString("b"), UserToken: sdkgo.NewSecretString("u"), AppToken: sdkgo.NewSecretString("a")},
	}, WithLogger(logs.Logger()))
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	runner, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{
		Connection: connection,
		ChannelThreadCreatedRoutes: []ChannelThreadCreatedTriggerRoute{{
			BindingName: "approval-start", Configuration: ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"},
			Target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil }),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	require.Eventually(t, func() bool {
		return len(logs.Find("slack socket mode connection failed; reconnecting", nil)) >= 1
	}, 3*time.Second, 10*time.Millisecond)
	cancel()
	require.ErrorIs(t, receiveRunResult(t, runFinished), context.Canceled)
	failure := logs.Find("slack socket mode connection failed; reconnecting", nil)[0]
	require.Equal(t, slog.LevelWarn, failure.Level)
	require.Contains(t, failure.Attrs["error"], "i/o timeout")
	require.Equal(t, "1", failure.Attrs["attempt"])
}

// TestSourceRecordsReportTheirCallerAsSource checks that a handler with AddSource sees the connector
// function that logged, not the logging helper.
func TestSourceRecordsReportTheirCallerAsSource(t *testing.T) {
	var output strings.Builder
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{},
		WithLogger(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{AddSource: true}))))
	require.NoError(t, err)
	source := client.channelThreadCreatedTriggerSource(sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"},
		ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"})
	source.log(context.Background(), slog.LevelInfo, "probe", source.connectionAttrs())
	var record struct {
		Source struct {
			Function string `json:"function"`
		} `json:"source"`
	}
	require.NoError(t, json.Unmarshal([]byte(output.String()), &record))
	require.Equal(t, "github.com/superdurable/dex-connectors-library/connectors/slack.TestSourceRecordsReportTheirCallerAsSource",
		record.Source.Function)
}

// TestSocketModeConnectionFailuresNameTheCause covers the causes an operator needs at 3am: Slack's error
// code and HTTP status, and a missing credential, without the token or the response body.
func TestSocketModeConnectionFailuresNameTheCause(t *testing.T) {
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	fullCredentials := Credentials{
		BotToken: sdkgo.NewSecretString(sentinelBotToken), UserToken: sdkgo.NewSecretString(sentinelUserToken),
		AppToken: sdkgo.NewSecretString(sentinelAppToken),
	}
	for _, testCase := range []struct {
		name        string
		status      int
		body        string
		credentials Credentials
		expected    string
	}{
		{"revoked token", http.StatusOK, `{"ok":false,"error":"token_revoked"}`, fullCredentials,
			"Slack rejected the Socket Mode connection: token_revoked (HTTP 200)"},
		{"rate limited HTML", http.StatusTooManyRequests, `<html>SENTINEL-BODY</html>`, fullCredentials,
			"Slack rejected the Socket Mode connection: unknown (HTTP 429)"},
		{"free-form error", http.StatusOK, `{"ok":false,"error":"SENTINEL body: see https://x"}`, fullCredentials,
			"Slack rejected the Socket Mode connection: unknown (HTTP 200)"},
		{"missing app token", http.StatusOK, `{"ok":true,"url":"ws://unused"}`,
			Credentials{BotToken: sdkgo.NewSecretString(sentinelBotToken), UserToken: sdkgo.NewSecretString(sentinelUserToken)},
			"Slack Socket Mode credentials are unavailable: credential app_token is required"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				_, _ = response.Write([]byte(testCase.body))
			}))
			defer server.Close()
			logs := testlog.NewLogRecorder()
			client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
				connectionReference: testCase.credentials,
			}, WithLogger(logs.Logger()))
			require.NoError(t, err)
			source := client.channelThreadCreatedTriggerSource(connectionReference, ChannelThreadCreatedTriggerConfiguration{ChannelID: "C123"})
			healthy, err := runMessageTriggerConnection(context.Background(), []messageTriggerRoute{{
				source: source, target: sdkgo.TriggerTargetFunc[MessageEvent](func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil }),
			}}, false)
			require.False(t, healthy)
			require.EqualError(t, err, testCase.expected)
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

// TestMessageTriggerDeliveriesLogIgnoredMessagesAtDebug covers the reason for every message a route ignores.
func TestMessageTriggerDeliveriesLogIgnoredMessagesAtDebug(t *testing.T) {
	logs := testlog.NewLogRecorder()
	client := &Client{logger: logs.Logger()}
	source := &messageTriggerSource{
		client: client, connection: sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}, channelID: "C123", requiresThread: true,
		matcher: MessageMatcher{MessageContains: "approve", PosterUserIDs: []string{"U2"}}, triggerName: "threadReplyCreated",
		bindingName: "approval-reply",
	}
	routes := []messageTriggerRoute{{source: source, target: sdkgo.TriggerTargetFunc[MessageEvent](
		func(context.Context, sdkgo.TriggerEvent[MessageEvent]) error { return nil },
	)}}
	reply := func(message messageEvent) messageEvent {
		message.Type, message.Timestamp, message.ThreadTS = "message", "2.0", "1.0"
		if message.Channel == "" {
			message.Channel = "C123"
		}
		return message
	}
	cases := []struct {
		reason  string
		message messageEvent
	}{
		{"not_a_message", messageEvent{Type: "reaction_added", Channel: "C123", User: "U2", Text: sentinelText}},
		{"subtype", reply(messageEvent{Subtype: "message_changed", User: "U2", Text: sentinelText + " approve"})},
		{"bot", reply(messageEvent{User: "U2", BotID: "B1", Text: sentinelText + " approve"})},
		{"missing_user", reply(messageEvent{Text: sentinelText + " approve"})},
		{"channel_mismatch", reply(messageEvent{Channel: "C999", User: "U2", Text: sentinelText + " approve"})},
		{"not_a_reply", messageEvent{Type: "message", Channel: "C123", User: "U2", Text: sentinelText + " approve", Timestamp: "1.0"}},
		{"matcher_mismatch", reply(messageEvent{User: "U3", Text: sentinelText + " approve"})},
	}
	for index, testCase := range cases {
		eventID := "Ev" + testCase.reason
		deliveries, err := decodeMessageTriggerDeliveries(context.Background(), routes, messageEnvelope(t, eventID, testCase.message))
		require.NoError(t, err)
		require.Empty(t, deliveries, testCase.reason)
		records := logs.Records()
		require.Len(t, records, index+1, testCase.reason)
		require.Equal(t, slog.LevelDebug, records[index].Level)
		require.Equal(t, "trigger event ignored", records[index].Message)
		require.Equal(t, testCase.reason, records[index].Attrs["reason"])
		require.Equal(t, eventID, records[index].Attrs["event_id"])
		require.Equal(t, "approval-reply", records[index].Attrs["binding"])
		require.Equal(t, "threadReplyCreated", records[index].Attrs["trigger"])
	}
	require.Equal(t, "message_changed", logs.Find("trigger event ignored", map[string]string{"reason": "subtype"})[0].Attrs["subtype"])
	require.Equal(t, "C999", logs.Find("trigger event ignored", map[string]string{"reason": "channel_mismatch"})[0].Attrs["channel"])

	deliveries, err := decodeMessageTriggerDeliveries(context.Background(), routes, socketEnvelope{Type: "hello"})
	require.NoError(t, err)
	require.Empty(t, deliveries)
	hello := logs.Find("slack socket mode envelope ignored", nil)
	require.Len(t, hello, 1)
	require.Equal(t, slog.LevelDebug, hello[0].Level)
	require.Equal(t, map[string]string{"connector": "slack", "connection": "workspace", "type": "hello", "envelope_id": ""}, hello[0].Attrs)

	deliveries, err = decodeMessageTriggerDeliveries(context.Background(), routes, messageEnvelope(t, "EvMatch", reply(messageEvent{User: "U2", Text: sentinelText + " approve"})))
	require.NoError(t, err)
	require.Len(t, deliveries, 1)
	require.Len(t, logs.Records(), len(cases)+1, "a matched message is not logged as ignored")
	requireNoSentinel(t, logs)

	// At INFO, ignored messages produce no records.
	quiet := testlog.NewLogRecorder()
	client.logger = slog.New(levelFilter{Handler: quiet, minimum: slog.LevelInfo})
	_, err = decodeMessageTriggerDeliveries(context.Background(), routes, messageEnvelope(t, "EvQuiet", cases[1].message))
	require.NoError(t, err)
	require.Empty(t, quiet.Records())
}

// levelFilter drops records below minimum, as a production handler at INFO does.
type levelFilter struct {
	slog.Handler
	minimum slog.Level
}

func (filter levelFilter) Enabled(_ context.Context, level slog.Level) bool {
	return level >= filter.minimum
}

func receiveEventIDWithin(t *testing.T, eventIDs <-chan string, timeout time.Duration) string {
	t.Helper()
	select {
	case eventID := <-eventIDs:
		return eventID
	case <-time.After(timeout):
		t.Fatal("Slack Trigger event was not delivered")
		return ""
	}
}

func flowNotActiveError() error {
	return &dex.FlowNotActiveError{ServiceError: &dex.ServiceError{
		Op: "InvokeRPC", FlowID: "slack-thread", Detail: "workflow execution already completed",
	}}
}

func newFakeSocketModeConnection(t *testing.T, dialer socketDialer, options ...Option) Connection {
	t.Helper()
	return newFakeSocketModeConnectionAt(t, "ws://socket.test", dialer, options...)
}

// newFakeSocketModeConnectionAt serves apps.connections.open with socketURL and dials through dialer.
func newFakeSocketModeConnectionAt(t *testing.T, socketURL string, dialer socketDialer, options ...Option) Connection {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"ok":true,"url":"` + socketURL + `"}`))
	}))
	t.Cleanup(server.Close)
	connectionReference := sdkgo.ConnectionRef{Provider: "slack", Name: "workspace"}
	options = append([]Option{func(options *clientOptions) { options.socketDialer = dialer }}, options...)
	client, err := New(Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{
		connectionReference: {
			BotToken: sdkgo.NewSecretString(sentinelBotToken), UserToken: sdkgo.NewSecretString(sentinelUserToken),
			AppToken: sdkgo.NewSecretString(sentinelAppToken),
		},
	}, options...)
	require.NoError(t, err)
	connection, err := NewConnection(client, connectionReference)
	require.NoError(t, err)
	return connection
}

func receiveAcknowledgement(t *testing.T, acknowledgements <-chan map[string]string) string {
	t.Helper()
	return receiveAcknowledgementWithin(t, acknowledgements, 2*time.Second)
}

func receiveAcknowledgementWithin(t *testing.T, acknowledgements <-chan map[string]string, timeout time.Duration) string {
	t.Helper()
	select {
	case acknowledgement := <-acknowledgements:
		return acknowledgement["envelope_id"]
	case <-time.After(timeout):
		t.Fatal("Slack envelope was not acknowledged")
		return ""
	}
}

func receiveEventID(t *testing.T, eventIDs <-chan string) string {
	t.Helper()
	select {
	case eventID := <-eventIDs:
		return eventID
	case <-time.After(2 * time.Second):
		t.Fatal("Slack Trigger event was not delivered")
		return ""
	}
}

func receiveRunResult(t *testing.T, runFinished <-chan error) error {
	t.Helper()
	select {
	case err := <-runFinished:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Slack Trigger runner did not stop")
		return nil
	}
}

func receiveTriggerEvent(t *testing.T, events <-chan sdkgo.TriggerEvent[MessageEvent]) sdkgo.TriggerEvent[MessageEvent] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(time.Second):
		t.Fatal("Slack Trigger event was not delivered")
		return sdkgo.TriggerEvent[MessageEvent]{}
	}
}

type fakeSocketConnection struct {
	envelopes        []socketEnvelope
	readErr          error
	writeErr         error
	acknowledgements chan map[string]string
}

func (connection *fakeSocketConnection) ReadJSON(destination any) error {
	if len(connection.envelopes) == 0 {
		if connection.readErr != nil {
			return connection.readErr
		}
		return errors.New("socket closed")
	}
	envelope := connection.envelopes[0]
	connection.envelopes = connection.envelopes[1:]
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, destination)
}

func (connection *fakeSocketConnection) WriteJSON(value any) error {
	if connection.writeErr != nil {
		return connection.writeErr
	}
	if connection.acknowledgements == nil {
		connection.acknowledgements = make(chan map[string]string, 1)
	}
	acknowledgement, ok := value.(map[string]string)
	if !ok {
		return errors.New("unexpected acknowledgement")
	}
	connection.acknowledgements <- acknowledgement
	return nil
}

func (*fakeSocketConnection) SetReadDeadline(time.Time) error { return nil }

func (*fakeSocketConnection) Close() error { return nil }

func messageEnvelope(t *testing.T, eventID string, message messageEvent) socketEnvelope {
	t.Helper()
	payload, err := json.Marshal(eventsAPIPayload{EventID: eventID, EventTime: 42, TeamID: "T1", Event: message})
	require.NoError(t, err)
	return socketEnvelope{EnvelopeID: "envelope-" + eventID, Type: "events_api", Payload: payload}
}
