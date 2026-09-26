//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadapproval

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/connectors/slack/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

const slackRunnerDeadline = 5 * time.Second

const (
	// slackPingInterval is how often the fake Slack pings each Socket Mode connection, as Slack's servers do.
	slackPingInterval = 10 * time.Second
	// quietConnectionWindow outlasts the runner's 45-second Socket Mode read deadline.
	quietConnectionWindow = 50 * time.Second
)

// Sentinels stand in for the Slack tokens and message text. No log record may contain them.
const (
	sentinelBotToken  = "SENTINEL-BOT-TOKEN"
	sentinelUserToken = "SENTINEL-USER-TOKEN"
	sentinelAppToken  = "SENTINEL-APP-TOKEN"
	sentinelText      = "SENTINEL-MESSAGE-TEXT"
)

// TestThreadApprovalRunnerSurvivesLateApprovalAndRestartWithRealDex runs README steps 8 and 9 through the
// shared Socket Mode runner wired as in main.go.
func TestThreadApprovalRunnerSurvivesLateApprovalAndRestartWithRealDex(t *testing.T) {
	fake := newSocketModeSlack(t)
	setup := newThreadApprovalRunnerSetup(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := func(threadTimestamp string) string {
		return fmt.Sprintf("slack-thread-approval-%s-C1-%s", teamID, threadTimestamp)
	}

	firstRun := setup.startRunner(t, ctx)
	fake.waitForConnection(t)
	fake.send(t, teamID, "EvRoot1-"+teamID, socketModeMessage("1.0", "", "U1", sentinelText+" request approval"))
	waitForSlackStatus(t, ctx, setup.harness.client, setup.flow, flowID("1.0"), StatusWaitingForReply)
	fake.send(t, teamID, "EvApprove1-"+teamID, socketModeMessage("1.1", "1.0", "U2", sentinelText+" approve"))
	result, err := setup.harness.client.WaitForFlow(ctx, flowID("1.0"), dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	require.Equal(t, 1, fake.postCount("1.0"))

	// Step 8: a second approval after completion is consumed and does not stop the shared socket.
	fake.send(t, teamID, "EvApprove1Late-"+teamID, socketModeMessage("1.2", "1.0", "U2", sentinelText+" approved, thanks"))
	fake.send(t, teamID, "EvRoot2-"+teamID, socketModeMessage("2.0", "", "U1", sentinelText+" request approval"))
	waitForSlackStatus(t, ctx, setup.harness.client, setup.flow, flowID("2.0"), StatusWaitingForReply)
	require.Empty(t, setup.pendingEventIDs(t))
	firstRun.stop(t)
	// The durable reply inbox logs the skipped second approval once, with its binding, the thread it
	// belongs to, the closed Flow, and the Dex reason.
	lateApproval := setup.logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": "EvApprove1Late-" + teamID})
	require.Len(t, lateApproval, 1)
	require.Equal(t, slog.LevelWarn, lateApproval[0].Level)
	require.Equal(t, map[string]string{
		"connector": "slack", "connection": ConnectionName, "trigger": "threadReplyCreated", "binding": ReplyTriggerBinding,
		"channel": "C1", "thread_ts": "1.0", "event_id": "EvApprove1Late-" + teamID, "flow_id": flowID("1.0"),
		"error": fmt.Sprintf("Trigger event is undeliverable: dex: InvokeRPC flow %q: NotFound: workflow execution already completed", flowID("1.0")),
	}, lateApproval[0].Attrs)
	require.Len(t, setup.logs.Find("trigger event skipped: undeliverable", nil), 1, "only the late approval is skipped")

	// Step 9: a restart replays past a reply that was persisted for the completed Flow before a crash.
	setup.persistReply(t, ctx, "EvApprove1AfterCrash-"+teamID, socketModeEvent(teamID, "1.3", "1.0", "U2", sentinelText+" approve"))
	require.Equal(t, []string{"EvApprove1AfterCrash-" + teamID}, setup.pendingEventIDs(t))
	secondRun := setup.startRunner(t, ctx)
	fake.waitForConnection(t)
	secondRun.requireRunning(t, 3*time.Second)
	fake.send(t, teamID, "EvRoot3-"+teamID, socketModeMessage("3.0", "", "U1", sentinelText+" request approval"))
	waitForSlackStatus(t, ctx, setup.harness.client, setup.flow, flowID("3.0"), StatusWaitingForReply)
	require.Equal(t, 1, fake.postCount("1.0"))
	require.Empty(t, setup.pendingEventIDs(t))
	replyBinding := map[string]string{"binding": ReplyTriggerBinding, "trigger": "threadReplyCreated"}
	require.Len(t, setup.logs.Find("replaying pending trigger events", withAttrs(replyBinding, "count", "1")), 1)
	require.Len(t, setup.logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": "EvApprove1AfterCrash-" + teamID}), 1)
	require.Len(t, setup.logs.Find("finished replaying pending trigger events",
		withAttrs(replyBinding, "delivered", "0", "skipped", "1", "remaining", "0")), 1)

	// A healthy connection stays open while no message arrives for longer than the 45-second read deadline:
	// Slack's pings extend it, so the quiet logs no reconnect.
	pongsBeforeQuiet := fake.pongCount()
	time.Sleep(quietConnectionWindow)
	require.GreaterOrEqual(t, fake.pongCount()-pongsBeforeQuiet, int(quietConnectionWindow/slackPingInterval)-1,
		"the runner answers every ping")
	require.Empty(t, setup.logs.Find("slack socket mode connection failed; reconnecting", nil))
	require.Len(t, setup.logs.Find("slack socket mode connected", nil), 2, "one connection per run")

	// While the Worker is down, the approval for thread 3 retries with backoff instead of being consumed.
	setup.harness.stopWorker(t)
	fake.send(t, teamID, "EvApprove3-"+teamID, socketModeMessage("3.1", "3.0", "U2", sentinelText+" approve"))
	approval3 := map[string]string{"event_id": "EvApprove3-" + teamID}
	require.Eventually(t, func() bool {
		return len(setup.logs.Find("trigger delivery failed; retrying", approval3)) >= 2
	}, 20*time.Second, 50*time.Millisecond)
	setup.harness.startWorker(t)
	result, err = setup.harness.client.WaitForFlow(ctx, flowID("3.0"), dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	require.Equal(t, 1, fake.postCount("3.0"))
	secondRun.stop(t)
	retries := setup.logs.Find("trigger delivery failed; retrying", approval3)
	schedule := []string{"250ms", "500ms", "1s", "2s", "4s", "8s", "16s", "30s"}
	for index, retry := range retries {
		require.Equal(t, slog.LevelWarn, retry.Level)
		require.Equal(t, strconv.Itoa(index+1), retry.Attrs["attempt"])
		require.Equal(t, schedule[min(index, len(schedule)-1)], retry.Attrs["delay"])
		require.Equal(t, withAttrs(replyBinding, "connector", "slack", "connection", ConnectionName, "channel", "C1",
			"thread_ts", "3.0", "event_id", "EvApprove3-"+teamID, "attempt", retry.Attrs["attempt"], "delay", retry.Attrs["delay"],
			"flow_id", flowID("3.0"), "error", retry.Attrs["error"]), retry.Attrs)
		require.Contains(t, retry.Attrs["error"], flowID("3.0"))
	}
	recovered := setup.logs.Find("trigger delivered after retry", approval3)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, strconv.Itoa(len(retries)+1), recovered[0].Attrs["attempts"])
	require.Len(t, setup.logs.Find("slack socket mode connected", nil), 2)
	require.Empty(t, setup.logs.Find("slack socket mode connection failed; reconnecting", nil))
	require.NotContains(t, setup.logs.Text(), "SENTINEL")
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

// TestThreadApprovalRunnerConsumesReplyWithoutFlowWithRealDex covers an approver's reply in a thread whose
// root never started a Flow.
func TestThreadApprovalRunnerConsumesReplyWithoutFlowWithRealDex(t *testing.T) {
	fake := newSocketModeSlack(t)
	setup := newThreadApprovalRunnerSetup(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)

	firstRun := setup.startRunner(t, ctx)
	fake.waitForConnection(t)
	fake.send(t, teamID, "EvOrphan-"+teamID, socketModeMessage("61.0", "60.0", "U2", sentinelText+" I approve of pizza friday"))
	fake.send(t, teamID, "EvRoot-"+teamID, socketModeMessage("70.0", "", "U1", sentinelText+" request approval"))
	waitForSlackStatus(t, ctx, setup.harness.client, setup.flow, fmt.Sprintf("slack-thread-approval-%s-C1-70.0", teamID), StatusWaitingForReply)
	require.Empty(t, setup.pendingEventIDs(t))
	firstRun.stop(t)
	orphan := setup.logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": "EvOrphan-" + teamID})
	require.Len(t, orphan, 1)
	require.Equal(t, ReplyTriggerBinding, orphan[0].Attrs["binding"])
	require.Contains(t, orphan[0].Attrs["error"], "workflow not found")

	secondRun := setup.startRunner(t, ctx)
	fake.waitForConnection(t)
	secondRun.requireRunning(t, 2*time.Second)
	secondRun.stop(t)
	require.Empty(t, setup.logs.Find("replaying pending trigger events", nil), "the restart finds no pending events")
	require.NotContains(t, setup.logs.Text(), "SENTINEL")
}

// TestThreadApprovalRunnerRetriesApprovalWhileThreadIsReadingWithRealDex covers an approval that reaches
// the Flow before ReadSlackThread finishes, live and in a crash-left root and reply pair that replay
// delivers back to back. Slack latency or a conversations.replies rate-limit backoff widens that window.
func TestThreadApprovalRunnerRetriesApprovalWhileThreadIsReadingWithRealDex(t *testing.T) {
	fake := newSocketModeSlack(t)
	// The read outlasts the RPC's wait for the state lock, so the approval reaches the handler while the
	// thread is still loading.
	fake.setRepliesDelay(8 * time.Second)
	setup := newThreadApprovalRunnerSetup(t, fake)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := func(threadTimestamp string) string {
		return fmt.Sprintf("slack-thread-approval-%s-C1-%s", teamID, threadTimestamp)
	}

	// Live: the approval arrives while the thread read is still in flight.
	firstRun := setup.startRunner(t, ctx)
	fake.waitForConnection(t)
	fake.send(t, teamID, "EvRootSlow-"+teamID, socketModeMessage("80.0", "", "U1", sentinelText+" request approval"))
	time.Sleep(time.Second)
	fake.send(t, teamID, "EvApproveSlow-"+teamID, socketModeMessage("80.1", "80.0", "U2", sentinelText+" approve"))
	requireSlackFlowCompleted(t, ctx, setup, flowID("80.0"))
	require.Equal(t, 1, fake.postCount("80.0"))
	approval := map[string]string{"event_id": "EvApproveSlow-" + teamID}
	retries := setup.logs.Find("trigger delivery failed; retrying", approval)
	require.NotEmpty(t, retries, "the early approval retries instead of being consumed")
	for _, retry := range retries {
		// While ReadSlackThread holds the state lock the RPC is aborted; once the lock is free but the
		// thread is not loaded yet, the handler keeps the reply pending with errThreadNotReady.
		require.Regexp(t, "attribute keys are locked|"+errThreadNotReady.Error(), retry.Attrs["error"])
	}
	require.Len(t, setup.logs.Find("trigger delivered after retry", approval), 1)
	require.Empty(t, setup.logs.Find("trigger event skipped: undeliverable", approval))
	firstRun.stop(t)

	// Replay: a crash left the root and its approval pending; the restart delivers them back to back.
	// A decoded root event carries its own timestamp as its thread timestamp.
	setup.persistRoot(t, ctx, "EvRootCrash-"+teamID, socketModeEvent(teamID, "90.0", "90.0", "U1", sentinelText+" request approval"))
	setup.persistReply(t, ctx, "EvApproveCrash-"+teamID, socketModeEvent(teamID, "90.1", "90.0", "U2", sentinelText+" approve"))
	require.ElementsMatch(t, []string{"EvRootCrash-" + teamID, "EvApproveCrash-" + teamID}, setup.pendingEventIDs(t))
	secondRun := setup.startRunner(t, ctx)
	requireSlackFlowCompleted(t, ctx, setup, flowID("90.0"))
	require.Equal(t, 1, fake.postCount("90.0"))
	require.Eventually(t, func() bool { return len(setup.pendingEventIDs(t)) == 0 }, slackRunnerDeadline, 50*time.Millisecond)
	secondRun.stop(t)
	replayed := setup.logs.Find("finished replaying pending trigger events",
		map[string]string{"binding": ReplyTriggerBinding, "delivered": "1", "skipped": "0", "remaining": "0"})
	require.Len(t, replayed, 1)
	require.Empty(t, setup.logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": "EvApproveCrash-" + teamID}))
	require.NotContains(t, setup.logs.Text(), "SENTINEL")
}

// requireSlackFlowCompleted fails within 30 seconds instead of waiting for the test deadline when an
// approval was lost and the Flow keeps waiting.
func requireSlackFlowCompleted(t *testing.T, ctx context.Context, setup *threadApprovalRunnerSetup, flowID string) {
	t.Helper()
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		// Replay may not have started the Flow yet, so a missing Flow is retried until the deadline.
		result, err := setup.harness.client.WaitForFlow(waitCtx, flowID, dex.WaitForFlowOptions{})
		if err == nil {
			require.Equal(t, dex.FlowCompleted, result.Status)
			return
		}
		if waitCtx.Err() != nil {
			require.NoError(t, err, "the Flow must complete; a lost approval leaves it waiting for a reply")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

type threadApprovalRunnerSetup struct {
	directory string
	store     *localconfig.Store
	flow      *Flow
	harness   *slackIntegrationHarness
	slack     *socketModeSlack
	logs      *testlog.LogRecorder
}

func newThreadApprovalRunnerSetup(t *testing.T, fake *socketModeSlack) *threadApprovalRunnerSetup {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": slack.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/slack",
			"moduleVersion": "v0.7.0", "provider": "slack", "connectionName": ConnectionName,
			"configuration": map[string]any{"endpoint": fake.URL},
			"credentials":   map[string]any{"bot_token": sentinelBotToken, "user_token": sentinelUserToken, "app_token": sentinelAppToken},
		}},
		"triggerBindings": []any{
			map[string]any{
				"connectorId": slack.ConnectorID, "connectionName": ConnectionName, "triggerName": "channelThreadCreated",
				"bindingName": StartTriggerBinding, "configuration": map[string]any{
					"channelId": "C1", "threadTriggerMatcher": map[string]any{"messageContains": "request approval"},
				},
			},
			map[string]any{
				"connectorId": slack.ConnectorID, "connectionName": ConnectionName, "triggerName": "threadReplyCreated",
				"bindingName": ReplyTriggerBinding, "configuration": map[string]any{
					"channelId": "C1", "threadReplyMatcher": map[string]any{"messageContains": "approve", "posterUserIds": []string{"U2"}},
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	flow, harness := newSlackIntegrationHarness(t, fake.URL)
	harness.startWorker(t)
	logs := testlog.NewLogRecorder()
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured Trigger logs:\n%s", logs.TextHandlerOutput())
		}
	})
	return &threadApprovalRunnerSetup{directory: directory, store: store, flow: flow, harness: harness, slack: fake, logs: logs}
}

// newRunner wires the shared runner exactly as main.go does, with the test's logger.
func (setup *threadApprovalRunnerSetup) newRunner(t *testing.T) *slack.MessageTriggerRunner {
	t.Helper()
	var startConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	require.NoError(t, setup.store.DecodeTriggerConfiguration(
		slack.ConnectorID, ConnectionName, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding, &startConfiguration,
	))
	startFilter, err := NewStartTriggerFilter(startConfiguration)
	require.NoError(t, err)
	var replyConfiguration slack.ThreadReplyCreatedTriggerConfiguration
	require.NoError(t, setup.store.DecodeTriggerConfiguration(
		slack.ConnectorID, ConnectionName, slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, ReplyTriggerBinding, &replyConfiguration,
	))
	replyFilter, err := NewReplyTriggerFilter(replyConfiguration)
	require.NoError(t, err)
	logger := setup.logs.Logger()
	runner, err := slack.NewLocalMessageTriggerRunner(setup.store, ConnectionName, slack.LocalMessageTriggerRunnerConfig{
		ChannelThreadCreatedRoutes: []slack.LocalChannelThreadCreatedTriggerRoute{{
			BindingName: StartTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(setup.harness.client, setup.flow, startFilter, ResolveFlowID, MapToFlowInput,
				sdkgo.WithTriggerLogger(logger.With("connector", slack.ConnectorID, "connection", ConnectionName,
					"trigger", slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, "binding", StartTriggerBinding))),
		}},
		ThreadReplyCreatedRoutes: []slack.LocalThreadReplyCreatedTriggerRoute{{
			BindingName: ReplyTriggerBinding,
			Target: sdkgo.NewDexRPCTriggerTarget(
				setup.harness.client, setup.flow.ReceiveThreadReply, replyFilter, ResolveFlowID, MapToReceiveThreadReplyInput,
				sdkgo.WithTriggerLogger(logger.With("connector", slack.ConnectorID, "connection", ConnectionName,
					"trigger", slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, "binding", ReplyTriggerBinding)),
			),
		}},
	}, slack.WithLogger(logger))
	require.NoError(t, err)
	return runner
}

func (setup *threadApprovalRunnerSetup) startRunner(t *testing.T, parent context.Context) *runningTriggerRunner {
	t.Helper()
	runner := setup.newRunner(t)
	ctx, cancel := context.WithCancel(parent)
	running := &runningTriggerRunner{cancel: cancel, closeTransport: setup.slack.dropConnections, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	return running
}

// persistReply stores a matched reply in the reply inbox without delivering it, as a crash after acknowledgement would.
func (setup *threadApprovalRunnerSetup) persistReply(t *testing.T, ctx context.Context, eventID string, event slack.MessageEvent) {
	t.Helper()
	setup.persist(t, ctx, slack.ThreadReplyCreatedTriggerDefinition.Trigger.TriggerName, ReplyTriggerBinding, eventID, event)
}

// persistRoot leaves a root message in the start binding's inbox, as a crash after acknowledgement would.
func (setup *threadApprovalRunnerSetup) persistRoot(t *testing.T, ctx context.Context, eventID string, event slack.MessageEvent) {
	t.Helper()
	setup.persist(t, ctx, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding, eventID, event)
}

func (setup *threadApprovalRunnerSetup) persist(
	t *testing.T, ctx context.Context, triggerName string, bindingName string, eventID string, event slack.MessageEvent,
) {
	t.Helper()
	inbox, err := localconfig.NewDurableTriggerTarget(
		setup.store, slack.ConnectorID, ConnectionName, triggerName, bindingName,
		sdkgo.TriggerTargetFunc[slack.MessageEvent](func(context.Context, sdkgo.TriggerEvent[slack.MessageEvent]) error { return nil }),
	)
	require.NoError(t, err)
	require.NoError(t, sdkgo.PrepareTriggerDelivery(ctx, inbox, sdkgo.TriggerEvent[slack.MessageEvent]{
		ID: eventID, OccurredAt: time.Now().UTC(), Payload: event,
	}))
}

func (setup *threadApprovalRunnerSetup) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(setup.directory, ".trigger-inbox-*.json"))
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

type runningTriggerRunner struct {
	cancel         context.CancelFunc
	closeTransport func()
	result         chan error
}

// requireRunning fails when Run returns during the window, for example because startup replay failed.
func (running *runningTriggerRunner) requireRunning(t *testing.T, window time.Duration) {
	t.Helper()
	select {
	case err := <-running.result:
		t.Fatalf("Slack Trigger runner returned early: %v", err)
	case <-time.After(window):
	}
}

// stop cancels Run. The fake Slack keeps pinging, which extends the runner's read deadline, so Run returns
// in time only because the runner closes its socket on cancellation. The fake then drops its own side of
// the connection too, so the stopped connection cannot take an envelope meant for the next run.
func (running *runningTriggerRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(slackRunnerDeadline):
		t.Fatal("Slack Trigger runner did not stop after cancellation")
	}
	running.closeTransport()
}

// socketModeSlack serves the Slack Web API and a real Socket Mode WebSocket that Slack-style pings every
// slackPingInterval.
type socketModeSlack struct {
	*httptest.Server
	envelopes   chan map[string]any
	acks        chan string
	connections chan struct{}
	mutex       sync.Mutex
	posts       map[string]int
	open        map[*websocket.Conn]bool
	pongs       int
	// repliesDelay slows conversations.replies, as real Slack latency or a rate-limit backoff does.
	repliesDelay time.Duration
}

func (fake *socketModeSlack) setRepliesDelay(delay time.Duration) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.repliesDelay = delay
}

func newSocketModeSlack(t *testing.T) *socketModeSlack {
	t.Helper()
	fake := &socketModeSlack{
		envelopes: make(chan map[string]any), acks: make(chan string, 32),
		connections: make(chan struct{}, 8), posts: map[string]int{}, open: map[*websocket.Conn]bool{},
	}
	upgrader := websocket.Upgrader{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apps.connections.open":
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"ok":true,"url":"ws://%s/socket"}`, request.Host)
		case "/socket":
			fake.serveSocket(upgrader, response, request)
		case "/conversations.replies":
			fake.mutex.Lock()
			delay := fake.repliesDelay
			fake.mutex.Unlock()
			time.Sleep(delay)
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"ok":true,"messages":[{"ts":%q,"user":"U1","text":"request approval"}]}`, request.URL.Query().Get("ts"))
		case "/chat.postMessage":
			var payload map[string]string
			if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
				http.Error(response, err.Error(), http.StatusBadRequest)
				return
			}
			fake.mutex.Lock()
			fake.posts[payload["thread_ts"]]++
			fake.mutex.Unlock()
			response.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(response, `{"ok":true,"channel":"C1","ts":"9.9","message":{"ts":"9.9","thread_ts":%q,"user":"UBOT","text":"Processing complete."}}`, payload["thread_ts"])
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(fake.Server.Close)
	t.Cleanup(func() { close(fake.envelopes) })
	return fake
}

func (fake *socketModeSlack) serveSocket(upgrader websocket.Upgrader, response http.ResponseWriter, request *http.Request) {
	connection, err := upgrader.Upgrade(response, request, nil)
	if err != nil {
		return
	}
	fake.mutex.Lock()
	fake.open[connection] = true
	fake.mutex.Unlock()
	defer func() {
		fake.mutex.Lock()
		delete(fake.open, connection)
		fake.mutex.Unlock()
		_ = connection.Close()
	}()
	if err := connection.WriteJSON(map[string]any{"type": "hello", "num_connections": 1}); err != nil {
		return
	}
	connection.SetPongHandler(func(string) error {
		fake.mutex.Lock()
		fake.pongs++
		fake.mutex.Unlock()
		return nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var acknowledgement map[string]string
			if err := connection.ReadJSON(&acknowledgement); err != nil {
				return
			}
			fake.acks <- acknowledgement["envelope_id"]
		}
	}()
	go func() {
		ticker := time.NewTicker(slackPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := connection.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(time.Second)); err != nil {
					return
				}
			}
		}
	}()
	fake.connections <- struct{}{}
	for {
		select {
		case <-done:
			return
		case envelope, ok := <-fake.envelopes:
			if !ok {
				return
			}
			if err := connection.WriteJSON(envelope); err != nil {
				return
			}
		}
	}
}

// dropConnections closes every open Socket Mode connection from the server side.
func (fake *socketModeSlack) dropConnections() {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	for connection := range fake.open {
		_ = connection.Close()
	}
}

func (fake *socketModeSlack) waitForConnection(t *testing.T) {
	t.Helper()
	select {
	case <-fake.connections:
	case <-time.After(slackRunnerDeadline):
		t.Fatal("Slack Trigger runner did not open a Socket Mode connection")
	}
}

// send writes one Events API envelope and waits for the runner to acknowledge it.
func (fake *socketModeSlack) send(t *testing.T, teamID string, eventID string, message map[string]any) {
	t.Helper()
	envelopeID := "env-" + eventID
	select {
	case fake.envelopes <- map[string]any{"envelope_id": envelopeID, "type": "events_api", "payload": map[string]any{
		"event_id": eventID, "event_time": time.Now().Unix(), "team_id": teamID, "event": message,
	}}:
	case <-time.After(slackRunnerDeadline):
		t.Fatalf("Slack Socket Mode reader did not accept envelope %s", envelopeID)
	}
	select {
	case acknowledgement := <-fake.acks:
		require.Equal(t, envelopeID, acknowledgement)
	case <-time.After(slackRunnerDeadline):
		t.Fatalf("Slack envelope %s was not acknowledged", envelopeID)
	}
}

// pongCount returns the number of pongs the runner sent in answer to the fake's pings.
func (fake *socketModeSlack) pongCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.pongs
}

func (fake *socketModeSlack) postCount(threadTimestamp string) int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.posts[threadTimestamp]
}

func socketModeMessage(timestamp string, threadTimestamp string, userID string, text string) map[string]any {
	message := map[string]any{"type": "message", "channel": "C1", "user": userID, "text": text, "ts": timestamp}
	if threadTimestamp != "" {
		message["thread_ts"] = threadTimestamp
	}
	return message
}

func socketModeEvent(teamID string, timestamp string, threadTimestamp string, userID string, text string) slack.MessageEvent {
	return slack.MessageEvent{
		TeamID: teamID, ChannelID: "C1", Timestamp: timestamp, ThreadTimestamp: threadTimestamp, UserID: userID, Text: text,
	}
}
