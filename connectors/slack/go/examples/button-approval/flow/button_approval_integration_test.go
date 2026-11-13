//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package buttonapproval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/superdurable/dex-connectors-library/connectors/slack/go"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/internal/testlog"
	"github.com/superdurable/dex-connectors-library/connectors/slack/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const fakeSlackDeadline = 5 * time.Second

// slackInteractiveAcknowledgementLimit is Slack's deadline for acknowledging a button click.
const slackInteractiveAcknowledgementLimit = 3 * time.Second

// Sentinels stand in for the Slack tokens, message text, and button value. No log record may contain them.
const (
	sentinelBotToken  = "SENTINEL-BOT-TOKEN"
	sentinelUserToken = "SENTINEL-USER-TOKEN"
	sentinelAppToken  = "SENTINEL-APP-TOKEN"
	sentinelText      = "SENTINEL-MESSAGE-TEXT"
)

var buttonApprovalScope = projectconfig.Scope{ProjectID: "slack-button-approval-tests", Kind: "live"}

// TestButtonApprovalDecidesOnceThroughSharedSocketWithRealDex runs the README flow through the shared Socket
// Mode runner wired as in main.go: a root message posts buttons, a non-decider's click is ignored, the
// approver's click completes the Flow, and a late click on the completed Flow is consumed.
func TestButtonApprovalDecidesOnceThroughSharedSocketWithRealDex(t *testing.T) {
	setup := newButtonApprovalSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := approvalFlowID(teamID, "C1", "10.0")
	run := setup.startRunner(t, ctx)
	setup.slack.waitForConnection(t)

	setup.slack.sendMessage(t, teamID, "EvRoot-"+teamID, "10.0", "U1", sentinelText+" request approval")
	state := waitForApprovalStatus(t, ctx, setup.client, setup.flow, flowID, StatusWaitingForDecision)
	buttons := setup.slack.buttonPost(t, "10.0")
	require.Equal(t, []string{ApproveActionID, RejectActionID}, buttons.actionIDs)
	require.Equal(t, approvalRequestText, buttons.text)
	require.Equal(t, buttons.timestamp, state.ButtonMessageTimestamp)

	// The click of a member who may not decide is filtered without changing the approval.
	setup.slack.sendClick(t, teamID, buttons.timestamp, "10.0", "U3", ApproveActionID, "11.0")
	// A matching button on another message in the thread, such as a copy, reaches the RPC but does not decide.
	foreignClickID := fmt.Sprintf("%s:C1:10.999999:%s:11.5", teamID, ApproveActionID)
	setup.slack.sendClick(t, teamID, "10.999999", "10.0", "U2", ApproveActionID, "11.5")
	require.Eventually(t, func() bool {
		return len(setup.logs.Find("trigger event delivered", map[string]string{"event_id": foreignClickID})) == 1
	}, 20*time.Second, 50*time.Millisecond)
	require.Equal(t, StatusWaitingForDecision, readApprovalState(t, ctx, setup.client, flowID).Status)
	setup.slack.sendClick(t, teamID, buttons.timestamp, "10.0", "U2", RejectActionID, "12.0")
	result, err := setup.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	require.Equal(t, []string{"Rejected by <@U2>."}, setup.slack.replyTexts("10.0"))
	state = readApprovalState(t, ctx, setup.client, flowID)
	require.Equal(t, DecisionRejected, state.Decision)
	require.Equal(t, "U2", state.DeciderUserID)

	// A later click on the completed approval is consumed and does not stop the shared socket.
	lateClickID := fmt.Sprintf("%s:C1:%s:%s:13.0", teamID, buttons.timestamp, ApproveActionID)
	setup.slack.sendClick(t, teamID, buttons.timestamp, "10.0", "U2", ApproveActionID, "13.0")
	require.Eventually(t, func() bool {
		return len(setup.logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": lateClickID})) == 1
	}, 20*time.Second, 50*time.Millisecond)
	secondFlowID := approvalFlowID(teamID, "C1", "20.0")
	stopFlowAtCleanup(t, setup.client, secondFlowID)
	setup.slack.sendMessage(t, teamID, "EvRoot2-"+teamID, "20.0", "U1", sentinelText+" request approval")
	waitForApprovalStatus(t, ctx, setup.client, setup.flow, secondFlowID, StatusWaitingForDecision)
	require.Equal(t, []string{"Rejected by <@U2>."}, setup.slack.replyTexts("10.0"), "the late click posts nothing")
	require.Empty(t, setup.pendingEventIDs(t))
	run.stop(t)
	require.Len(t, setup.logs.Find("slack socket mode connected", nil), 1, "message and button routes share one connection")
	require.NotContains(t, setup.logs.Text(), "SENTINEL")
}

// TestButtonApprovalAcknowledgesClicksWhileWorkerIsDownWithRealDex covers Slack's three-second limit while the
// Worker is stopped: the approval's delivery keeps retrying, yet the click and a later root message are each
// acknowledged at once, and the recorded click completes the Flow after the Worker returns.
func TestButtonApprovalAcknowledgesClicksWhileWorkerIsDownWithRealDex(t *testing.T) {
	setup := newButtonApprovalSetup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	teamID := "T" + strconv.FormatInt(time.Now().UnixNano(), 10)
	flowID := approvalFlowID(teamID, "C1", "30.0")
	run := setup.startRunner(t, ctx)
	setup.slack.waitForConnection(t)
	setup.slack.sendMessage(t, teamID, "EvRoot-"+teamID, "30.0", "U1", sentinelText+" request approval")
	waitForApprovalStatus(t, ctx, setup.client, setup.flow, flowID, StatusWaitingForDecision)
	buttons := setup.slack.buttonPost(t, "30.0")

	setup.stopWorker(t)
	clickID := fmt.Sprintf("%s:C1:%s:%s:31.0", teamID, buttons.timestamp, ApproveActionID)
	elapsed := setup.slack.sendClick(t, teamID, buttons.timestamp, "30.0", "U2", ApproveActionID, "31.0")
	require.Less(t, elapsed, slackInteractiveAcknowledgementLimit)
	require.Eventually(t, func() bool {
		return len(setup.logs.Find("trigger delivery failed; retrying", map[string]string{"event_id": clickID})) >= 1
	}, 30*time.Second, 50*time.Millisecond, "the click's delivery retries while the Worker is down")
	require.Equal(t, []string{clickID}, setup.pendingEventIDs(t), "the acknowledged click is recorded in its durable inbox")
	// The read loop keeps reading while that delivery retries.
	laterFlowID := approvalFlowID(teamID, "C1", "40.0")
	stopFlowAtCleanup(t, setup.client, laterFlowID)
	elapsed = setup.slack.sendMessage(t, teamID, "EvRoot2-"+teamID, "40.0", "U1", sentinelText+" request approval")
	require.Less(t, elapsed, slackInteractiveAcknowledgementLimit)

	setup.startWorker(t)
	result, err := setup.client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
	require.NoError(t, err)
	require.Equal(t, dex.FlowCompleted, result.Status)
	require.Equal(t, []string{"Approved by <@U2>."}, setup.slack.replyTexts("30.0"))
	waitForApprovalStatus(t, ctx, setup.client, setup.flow, laterFlowID, StatusWaitingForDecision)
	require.Len(t, setup.logs.Find("trigger delivered after retry", map[string]string{"event_id": clickID}), 1)
	require.Empty(t, setup.logs.Find("slack interactive envelope acknowledged late", nil))
	require.Eventually(t, func() bool { return len(setup.pendingEventIDs(t)) == 0 }, 10*time.Second, 50*time.Millisecond)
	run.stop(t)
	require.NotContains(t, setup.logs.Text(), "SENTINEL")
}

type buttonApprovalSetup struct {
	slack         *socketModeSlack
	configuration projectconfig.Configuration
	objects       *testsupport.ObjectStore
	logs          *testlog.LogRecorder
	connection    slack.Connection
	flow          *ButtonApprovalFlow
	registry      *dex.Registry
	cache         *blobcache.Cache
	client        *dex.Client
	serverAddress string
	worker        *dex.Worker
	workerResult  chan error
}

func newButtonApprovalSetup(t *testing.T) *buttonApprovalSetup {
	t.Helper()
	fake := newSocketModeSlack(t)
	logs := testlog.NewLogRecorder()
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured Trigger logs:\n%s", logs.TextHandlerOutput())
		}
	})
	reference := sdkgo.ConnectionRef{Provider: "slack", Name: ConnectionName}
	providerClient, err := slack.New(slack.Config{Endpoint: fake.URL}, sdkgo.StaticCredentialProvider[slack.Credentials]{reference: {
		BotToken: sdkgo.NewSecretString(sentinelBotToken), UserToken: sdkgo.NewSecretString(sentinelUserToken),
		AppToken: sdkgo.NewSecretString(sentinelAppToken),
	}}, slack.WithLogger(logs.Logger()))
	require.NoError(t, err)
	connection, err := slack.NewConnection(providerClient, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	setup := &buttonApprovalSetup{
		slack: fake, objects: testsupport.NewObjectStore(), logs: logs, connection: connection, flow: flow,
		registry: registry, cache: cache, serverAddress: environmentOr("DEX_FLOW_SERVICE_ADDRESS", "127.0.0.1:8801"),
		configuration: projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{
			{ConnectorID: slack.ConnectorID, ConnectionName: ConnectionName, TriggerName: "channelThreadCreated", BindingName: StartTriggerBinding,
				Configuration: json.RawMessage(`{"channelId":"C1","threadTriggerMatcher":{"messageContains":"request approval"}}`)},
			{ConnectorID: slack.ConnectorID, ConnectionName: ConnectionName, TriggerName: "buttonClicked", BindingName: ButtonTriggerBinding,
				Configuration: json.RawMessage(`{"channelId":"C1","clickerUserIds":["U2"]}`)},
		}},
	}
	setup.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: setup.serverAddress})
	require.NoError(t, err)
	t.Cleanup(func() {
		if setup.worker != nil {
			setup.stopWorker(t)
		}
		require.NoError(t, errors.Join(setup.client.Close(), setup.cache.Close()))
	})
	setup.startWorker(t)
	return setup
}

// newRunner wires the shared runner as main.go and NewProjectMessageTriggerRunner do, with the bindings'
// durable inboxes in memory.
func (setup *buttonApprovalSetup) newRunner(t *testing.T) *slack.MessageTriggerRunner {
	t.Helper()
	var startConfiguration slack.ChannelThreadCreatedTriggerConfiguration
	require.NoError(t, setup.configuration.DecodeTriggerConfiguration(slack.ConnectorID, ConnectionName,
		slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding, &startConfiguration))
	startFilter, err := NewStartTriggerFilter(startConfiguration)
	require.NoError(t, err)
	var buttonConfiguration slack.ButtonClickedTriggerConfiguration
	require.NoError(t, setup.configuration.DecodeTriggerConfiguration(slack.ConnectorID, ConnectionName,
		slack.ButtonClickedTriggerDefinition.Trigger.TriggerName, ButtonTriggerBinding, &buttonConfiguration))
	buttonFilter, err := NewButtonTriggerFilter(buttonConfiguration)
	require.NoError(t, err)
	logger := setup.logs.Logger()
	startKey, startInbox := setup.inbox(t, slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding)
	startTarget, err := provider.NewDurableTriggerTarget(startInbox, startKey,
		sdkgo.NewDexFlowTriggerTarget(setup.client, setup.flow, startFilter, ResolveFlowID, MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
		provider.WithTriggerLogger(logger))
	require.NoError(t, err)
	buttonKey, buttonInbox := setup.inbox(t, slack.ButtonClickedTriggerDefinition.Trigger.TriggerName, ButtonTriggerBinding)
	buttonTarget, err := provider.NewDurableTriggerTarget(buttonInbox, buttonKey,
		sdkgo.NewDexRPCTriggerTarget(setup.client, setup.flow.ReceiveButtonDecision, buttonFilter, ResolveButtonFlowID,
			MapToReceiveButtonDecisionInput, sdkgo.WithTriggerLogger(logger)),
		provider.WithTriggerLogger(logger))
	require.NoError(t, err)
	runner, err := slack.NewMessageTriggerRunner(slack.MessageTriggerRunnerConfig{
		Connection: setup.connection,
		ChannelThreadCreatedRoutes: []slack.ChannelThreadCreatedTriggerRoute{{
			BindingName: StartTriggerBinding, Configuration: startConfiguration, Target: startTarget,
		}},
		ButtonClickedRoutes: []slack.ButtonClickedTriggerRoute{{
			BindingName: ButtonTriggerBinding, Configuration: buttonConfiguration, Target: buttonTarget,
		}},
	})
	require.NoError(t, err)
	return runner
}

func (setup *buttonApprovalSetup) inbox(t *testing.T, triggerName string, bindingName string) (projectconfig.TriggerInboxKey, *projectconfig.TriggerInbox) {
	t.Helper()
	key := projectconfig.TriggerInboxKey{ConnectorID: slack.ConnectorID, ConnectionName: ConnectionName, TriggerName: triggerName, BindingName: bindingName}
	inbox, err := projectconfig.NewTriggerInbox(setup.objects, buttonApprovalScope, key)
	require.NoError(t, err)
	return key, inbox
}

// pendingEventIDs lists the event IDs waiting in both bindings' durable inboxes.
func (setup *buttonApprovalSetup) pendingEventIDs(t *testing.T) []string {
	t.Helper()
	eventIDs := []string{}
	for _, binding := range []struct{ triggerName, bindingName string }{
		{slack.ChannelThreadCreatedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding},
		{slack.ButtonClickedTriggerDefinition.Trigger.TriggerName, ButtonTriggerBinding},
	} {
		_, inbox := setup.inbox(t, binding.triggerName, binding.bindingName)
		pending, err := inbox.Pending(context.Background())
		require.NoError(t, err)
		for _, event := range pending {
			eventIDs = append(eventIDs, event.ID)
		}
	}
	return eventIDs
}

func (setup *buttonApprovalSetup) startWorker(t *testing.T) {
	t.Helper()
	worker, err := dex.NewWorker(setup.registry, setup.cache, dex.WorkerOptions{FlowServiceAddress: setup.serverAddress})
	require.NoError(t, err)
	setup.worker = worker
	setup.workerResult = make(chan error, 1)
	go func() { setup.workerResult <- worker.Start() }()
	registrationContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	require.NoError(t, worker.WaitPullRegistered(registrationContext))
}

func (setup *buttonApprovalSetup) stopWorker(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, errors.Join(setup.worker.Stop(ctx), <-setup.workerResult))
	setup.worker = nil
	setup.workerResult = nil
}

type runningTriggerRunner struct {
	cancel         context.CancelFunc
	closeTransport func()
	result         chan error
}

func (setup *buttonApprovalSetup) startRunner(t *testing.T, parent context.Context) *runningTriggerRunner {
	t.Helper()
	runner := setup.newRunner(t)
	ctx, cancel := context.WithCancel(parent)
	running := &runningTriggerRunner{cancel: cancel, closeTransport: setup.slack.dropConnections, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	return running
}

func (running *runningTriggerRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(fakeSlackDeadline):
		t.Fatal("Slack Trigger runner did not stop after cancellation")
	}
	running.closeTransport()
}

func waitForApprovalStatus(t *testing.T, ctx context.Context, client *dex.Client, flow *ButtonApprovalFlow, flowID string, expected Status) ApprovalState {
	t.Helper()
	var state ApprovalState
	require.Eventually(t, func() bool {
		state = ApprovalState{}
		return client.InvokeRPC(ctx, flowID, flow.GetApprovalStatus, nil, &state) == nil && state.Status == expected
	}, 20*time.Second, 100*time.Millisecond)
	return state
}

// readApprovalState reads the retained Attribute, which a completed Flow still serves.
func readApprovalState(t *testing.T, ctx context.Context, client *dex.Client, flowID string) ApprovalState {
	t.Helper()
	var state ApprovalState
	result, err := client.GetAttributes(ctx, flowID, dex.ReadAttribute(approvalStateAttribute, &state))
	require.NoError(t, err)
	require.True(t, result.IsSet(approvalStateAttribute))
	return state
}

func stopFlowAtCleanup(t *testing.T, client *dex.Client, flowID string) {
	t.Helper()
	t.Cleanup(func() {
		stopContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := client.StopFlow(stopContext, flowID, dex.StopOptions{Type: dex.CancelFlow, Reason: "integration test finished"})
		var serviceError *dex.ServiceError
		if errors.As(err, &serviceError) && serviceError.SubStatus == dex.ErrorSubStatusFlowNotFound {
			return
		}
		require.NoError(t, err)
	})
}

func environmentOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// socketModeSlack serves the Slack Web API and a real Socket Mode WebSocket.
type socketModeSlack struct {
	*httptest.Server
	envelopes   chan map[string]any
	acks        chan string
	connections chan struct{}
	mutex       sync.Mutex
	open        map[*websocket.Conn]bool
	buttonPosts map[string]buttonMessagePost
	replies     map[string][]string
	nextPost    int
}

type buttonMessagePost struct {
	timestamp string
	text      string
	actionIDs []string
}

func newSocketModeSlack(t *testing.T) *socketModeSlack {
	t.Helper()
	fake := &socketModeSlack{
		envelopes: make(chan map[string]any), acks: make(chan string, 32), connections: make(chan struct{}, 8),
		open: map[*websocket.Conn]bool{}, buttonPosts: map[string]buttonMessagePost{}, replies: map[string][]string{},
	}
	upgrader := websocket.Upgrader{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/apps.connections.open":
			_, _ = fmt.Fprintf(response, `{"ok":true,"url":"ws://%s/socket"}`, request.Host)
		case "/socket":
			fake.serveSocket(upgrader, response, request)
		case "/chat.postMessage":
			fake.servePost(response, request)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(fake.Server.Close)
	t.Cleanup(func() { close(fake.envelopes) })
	return fake
}

func (fake *socketModeSlack) servePost(response http.ResponseWriter, request *http.Request) {
	var payload struct {
		Channel  string `json:"channel"`
		Text     string `json:"text"`
		ThreadTS string `json:"thread_ts"`
		Blocks   []struct {
			Type     string `json:"type"`
			Elements []struct {
				ActionID string `json:"action_id"`
			} `json:"elements"`
		} `json:"blocks"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	fake.mutex.Lock()
	fake.nextPost++
	timestamp := fmt.Sprintf("%s.%06d", payload.ThreadTS, fake.nextPost)
	if len(payload.Blocks) > 0 {
		post := buttonMessagePost{timestamp: timestamp, text: payload.Text}
		for _, block := range payload.Blocks {
			for _, element := range block.Elements {
				post.actionIDs = append(post.actionIDs, element.ActionID)
			}
		}
		fake.buttonPosts[payload.ThreadTS] = post
	} else {
		fake.replies[payload.ThreadTS] = append(fake.replies[payload.ThreadTS], payload.Text)
	}
	fake.mutex.Unlock()
	encoded, _ := json.Marshal(map[string]any{"ok": true, "channel": payload.Channel, "ts": timestamp, "message": map[string]any{
		"ts": timestamp, "thread_ts": payload.ThreadTS, "user": "UBOT", "text": payload.Text,
	}})
	_, _ = response.Write(encoded)
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
	case <-time.After(fakeSlackDeadline):
		t.Fatal("Slack Trigger runner did not open a Socket Mode connection")
	}
}

// send writes one envelope and returns how long the runner took to acknowledge it.
func (fake *socketModeSlack) send(t *testing.T, envelope map[string]any) time.Duration {
	t.Helper()
	envelopeID := envelope["envelope_id"].(string)
	select {
	case fake.envelopes <- envelope:
	case <-time.After(fakeSlackDeadline):
		t.Fatalf("Slack Socket Mode reader did not accept envelope %s", envelopeID)
	}
	sentAt := time.Now()
	select {
	case acknowledgement := <-fake.acks:
		require.Equal(t, envelopeID, acknowledgement)
		return time.Since(sentAt)
	case <-time.After(fakeSlackDeadline):
		t.Fatalf("Slack envelope %s was not acknowledged", envelopeID)
		return 0
	}
}

func (fake *socketModeSlack) sendMessage(t *testing.T, teamID string, eventID string, timestamp string, userID string, text string) time.Duration {
	t.Helper()
	return fake.send(t, map[string]any{"envelope_id": "env-" + eventID, "type": "events_api", "payload": map[string]any{
		"event_id": eventID, "event_time": time.Now().Unix(), "team_id": teamID,
		"event": map[string]any{"type": "message", "channel": "C1", "user": userID, "text": text, "ts": timestamp},
	}})
}

// sendClick writes one block_actions envelope with Slack's documented fields and the assumed thread_ts fields.
func (fake *socketModeSlack) sendClick(t *testing.T, teamID string, messageTimestamp string, threadTimestamp string, userID string, actionID string, actionTimestamp string) time.Duration {
	t.Helper()
	return fake.send(t, map[string]any{"envelope_id": "env-click-" + teamID + "-" + actionTimestamp, "type": "interactive", "accepts_response_payload": false,
		"payload": map[string]any{
			"type": "block_actions", "trigger_id": "SENTINEL-TRIGGER-ID", "response_url": "https://hooks.slack.com/actions/SENTINEL",
			"team": map[string]any{"id": teamID}, "user": map[string]any{"id": userID}, "channel": map[string]any{"id": "C1"},
			"container": map[string]any{"type": "message", "message_ts": messageTimestamp, "channel_id": "C1", "thread_ts": threadTimestamp},
			"message":   map[string]any{"type": "message", "ts": messageTimestamp, "thread_ts": threadTimestamp, "text": sentinelText},
			"actions": []any{map[string]any{
				"type": "button", "action_id": actionID, "block_id": "decision", "value": threadTimestamp, "action_ts": actionTimestamp,
			}},
		}})
}

func (fake *socketModeSlack) buttonPost(t *testing.T, threadTimestamp string) buttonMessagePost {
	t.Helper()
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	post, found := fake.buttonPosts[threadTimestamp]
	require.True(t, found, "no button message was posted in thread %s", threadTimestamp)
	return post
}

func (fake *socketModeSlack) replyTexts(threadTimestamp string) []string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]string(nil), fake.replies[threadTimestamp]...)
}
