//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package threadreply

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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/connectors/google/gmail/internal/testlog"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestThreadReplyRunnerSurvivesReplyAfterCompletionAndRestartWithRealDex drives the example Flow through the
// shared ordered runner wired as in main.go.
func TestThreadReplyRunnerSurvivesReplyAfterCompletionAndRestartWithRealDex(t *testing.T) {
	provider := newGmailInboxProvider(t)
	flow, harness := newGmailIntegrationHarness(t, provider.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	store, directory := newGmailRunnerStore(t, provider.URL)
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadA := "thread-a-" + testRunID
	threadB := "thread-b-" + testRunID
	flowID := func(threadID string) string { return fmt.Sprintf("gmail-thread-reply-%s-%s", ConnectionName, threadID) }
	logs := newGmailRunnerLogs(t)

	firstRun := startGmailRunner(t, ctx, store, harness, flow, logs)
	provider.receive(gmailInboxMessage{id: "root-a-" + testRunID, threadID: threadA})
	waitForGmailStatus(t, ctx, harness.client, flow, flowID(threadA), StatusWaitingForReply)
	provider.receive(gmailInboxMessage{id: "reply-a1-" + testRunID, threadID: threadA, reply: true})
	requireGmailFlowCompleted(t, ctx, harness.client, flowID(threadA))
	require.Equal(t, 1, provider.sendCount(threadA))

	// A later reply in the completed thread is consumed instead of stopping the reply route.
	provider.receive(gmailInboxMessage{id: "reply-a2-" + testRunID, threadID: threadA, reply: true})
	provider.waitForCompletedPoll(t)
	require.Empty(t, pendingGmailRunnerEventIDs(t, directory))
	require.Equal(t, 1, provider.sendCount(threadA))
	// The durable reply inbox logs the skipped reply once, with its binding, the thread the poller passes
	// along, the closed Flow, and the Dex reason.
	lateReply := logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": "reply-a2-" + testRunID})
	require.Len(t, lateReply, 1)
	require.Equal(t, slog.LevelWarn, lateReply[0].Level)
	require.Equal(t, map[string]string{
		"connector": "gmail", "connection": ConnectionName, "trigger": "replyReceived", "binding": ReplyTriggerBinding,
		"thread_id": threadA, "event_id": "reply-a2-" + testRunID, "flow_id": flowID(threadA),
		"error": fmt.Sprintf("Trigger event is undeliverable: dex: InvokeRPC flow %q: NotFound: workflow execution already completed", flowID(threadA)),
	}, lateReply[0].Attrs)
	require.Len(t, logs.Find("trigger event skipped: undeliverable", nil), 1, "only the late reply is skipped")

	// The root and reply of a new thread arrive in the same page; the root is delivered first. The Flow's
	// read of the root is slow, so the reply reaches the RPC while the Flow is still loading and must be retried.
	provider.slowFullRead("root-b-"+testRunID, 3*time.Second)
	provider.receive(
		gmailInboxMessage{id: "root-b-" + testRunID, threadID: threadB},
		gmailInboxMessage{id: "reply-b-" + testRunID, threadID: threadB, reply: true},
	)
	requireGmailFlowCompleted(t, ctx, harness.client, flowID(threadB))
	require.Equal(t, 1, provider.sendCount(threadB))
	provider.waitForCompletedPoll(t)
	require.Empty(t, pendingGmailRunnerEventIDs(t, directory))
	firstRun.stop(t)
	// The early reply retried once per poll, with the poll interval as its delay, until the Flow waited.
	replyB := map[string]string{"event_id": "reply-b-" + testRunID}
	retries := logs.Find("trigger delivery failed; retrying", replyB)
	require.NotEmpty(t, retries)
	for index, retry := range retries {
		require.Equal(t, slog.LevelWarn, retry.Level)
		require.Equal(t, strconv.Itoa(index+1), retry.Attrs["attempt"])
		require.Equal(t, "1s", retry.Attrs["delay"])
		require.Equal(t, ReplyTriggerBinding, retry.Attrs["binding"])
		require.Equal(t, threadB, retry.Attrs["thread_id"])
		require.Equal(t, flowID(threadB), retry.Attrs["flow_id"])
		// The RPC waits either for the Flow's lock on its state or for the Flow to finish reading the root.
		require.Contains(t, retry.Attrs["error"], flowID(threadB))
	}
	recovered := logs.Find("trigger delivered after retry", replyB)
	require.Len(t, recovered, 1)
	require.Equal(t, slog.LevelInfo, recovered[0].Level)
	require.Equal(t, strconv.Itoa(len(retries)+1), recovered[0].Attrs["attempts"])

	// A restart rescans the whole page. Every event is consumed and the runner keeps polling.
	secondRun := startGmailRunner(t, ctx, store, harness, flow, logs)
	provider.waitForCompletedPoll(t)
	provider.waitForCompletedPoll(t)
	secondRun.requireRunning(t)
	require.Empty(t, pendingGmailRunnerEventIDs(t, directory))
	require.Equal(t, 1, provider.sendCount(threadA))
	require.Equal(t, 1, provider.sendCount(threadB))
	secondRun.stop(t)
	// The rescan redelivers every message: roots are duplicate starts, and replies to completed Flows are skipped.
	for _, eventID := range []string{"reply-a1-" + testRunID, "reply-b-" + testRunID} {
		require.Len(t, logs.Find("trigger event skipped: undeliverable", map[string]string{"event_id": eventID}), 1, eventID)
	}
	for _, eventID := range []string{"root-a-" + testRunID, "root-b-" + testRunID} {
		for _, duplicate := range []string{"false", "true"} {
			require.Len(t, logs.Find("trigger event delivered", map[string]string{
				"event_id": eventID, "target": "flow_start", "duplicate": duplicate,
			}), 1, eventID+" duplicate="+duplicate)
		}
	}
	require.NotContains(t, logs.Text(), "SENTINEL")
	require.NotContains(t, logs.Text(), "@example.com", "records never contain a sender address")
}

// newGmailRunnerLogs records the runner's logs and writes them to the test log when the test fails or runs
// with -v.
func newGmailRunnerLogs(t *testing.T) *testlog.LogRecorder {
	t.Helper()
	logs := testlog.NewLogRecorder()
	t.Cleanup(func() {
		if t.Failed() || testing.Verbose() {
			t.Logf("captured Trigger logs:\n%s", logs.TextHandlerOutput())
		}
	})
	return logs
}

// TestThreadReplyRunnerDeliversRootBeforeReplyThatArrivesDuringPollWithRealDex adds a root and its reply right
// after the runner lists the root route. The runner has already listed the reply route in that poll, so both
// wait for the next poll, where the root starts the Flow before the reply reaches it.
func TestThreadReplyRunnerDeliversRootBeforeReplyThatArrivesDuringPollWithRealDex(t *testing.T) {
	provider := newGmailInboxProvider(t)
	testRunID := strconv.FormatInt(time.Now().UnixNano(), 10)
	threadC := "thread-c-" + testRunID
	var armed atomic.Bool
	proxy := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		isRootListing := request.Method == http.MethodGet && request.URL.Path == "/users/me/messages" &&
			request.URL.Query().Get("q") == "root-query"
		provider.Config.Handler.ServeHTTP(response, request)
		if isRootListing && armed.CompareAndSwap(true, false) {
			provider.receive(
				gmailInboxMessage{id: "root-c-" + testRunID, threadID: threadC},
				gmailInboxMessage{id: "reply-c-" + testRunID, threadID: threadC, reply: true},
			)
		}
	}))
	t.Cleanup(proxy.Close)
	flow, harness := newGmailIntegrationHarness(t, proxy.URL)
	harness.startWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, directory := newGmailRunnerStoreWithQueries(t, proxy.URL, "root-query", "reply-query")
	logs := newGmailRunnerLogs(t)
	run := startGmailRunner(t, ctx, store, harness, flow, logs)
	provider.waitForCompletedPoll(t)
	armed.Store(true)

	requireGmailFlowCompleted(t, ctx, harness.client, fmt.Sprintf("gmail-thread-reply-%s-%s", ConnectionName, threadC))
	require.Equal(t, 1, provider.sendCount(threadC))
	provider.waitForCompletedPoll(t)
	require.Empty(t, pendingGmailRunnerEventIDs(t, directory))
	run.stop(t)
	require.Empty(t, logs.Find("trigger event skipped: undeliverable", nil), "the reply never overtakes its root")
	require.NotContains(t, logs.Text(), "SENTINEL")
}

// requireGmailFlowCompleted waits for the poller to start the Flow, then for the Flow to complete.
func requireGmailFlowCompleted(t *testing.T, ctx context.Context, client *dex.Client, flowID string) {
	t.Helper()
	var lastErr error
	var status dex.FlowStatus
	require.Eventually(t, func() bool {
		result, err := client.WaitForFlow(ctx, flowID, dex.WaitForFlowOptions{})
		lastErr, status = err, result.Status
		return err == nil
	}, time.Minute, 200*time.Millisecond, "wait for Flow %s", flowID)
	require.NoError(t, lastErr)
	require.Equal(t, dex.FlowCompleted, status)
}

type runningGmailRunner struct {
	cancel context.CancelFunc
	result chan error
}

// startGmailRunner wires the shared local runner exactly as main.go does, with the test's logger.
func startGmailRunner(
	t *testing.T,
	parent context.Context,
	store *localconfig.Store,
	harness *gmailIntegrationHarness,
	flow *Flow,
	logs *testlog.LogRecorder,
) *runningGmailRunner {
	t.Helper()
	var startConfiguration gmail.MessageReceivedTriggerConfiguration
	require.NoError(t, store.DecodeTriggerConfiguration(
		gmail.ConnectorID, ConnectionName, gmail.MessageReceivedTriggerDefinition.Trigger.TriggerName, StartTriggerBinding, &startConfiguration,
	))
	startFilter, err := NewStartTriggerFilter(startConfiguration)
	require.NoError(t, err)
	var replyConfiguration gmail.ReplyReceivedTriggerConfiguration
	require.NoError(t, store.DecodeTriggerConfiguration(
		gmail.ConnectorID, ConnectionName, gmail.ReplyReceivedTriggerDefinition.Trigger.TriggerName, ReplyTriggerBinding, &replyConfiguration,
	))
	replyFilter, err := NewReplyTriggerFilter(replyConfiguration)
	require.NoError(t, err)
	logger := logs.Logger()
	runner, err := gmail.NewLocalMessageTriggerRunner(store, ConnectionName, gmail.LocalMessageTriggerRunnerConfig{
		MessageReceivedRoutes: []gmail.LocalMessageReceivedTriggerRoute{{
			BindingName: StartTriggerBinding,
			Target: sdkgo.NewDexFlowTriggerTarget(harness.client, flow, startFilter, ResolveFlowID, MapToFlowInput,
				sdkgo.WithTriggerLogger(logger.With("connector", gmail.ConnectorID, "connection", ConnectionName,
					"trigger", gmail.MessageReceivedTriggerDefinition.Trigger.TriggerName, "binding", StartTriggerBinding))),
		}},
		ReplyReceivedRoutes: []gmail.LocalReplyReceivedTriggerRoute{{
			BindingName: ReplyTriggerBinding,
			Target: sdkgo.NewDexRPCTriggerTarget(
				harness.client, flow.ReceiveEmailReply, replyFilter, ResolveFlowID, MapToReceiveEmailReplyInput,
				sdkgo.WithTriggerLogger(logger.With("connector", gmail.ConnectorID, "connection", ConnectionName,
					"trigger", gmail.ReplyReceivedTriggerDefinition.Trigger.TriggerName, "binding", ReplyTriggerBinding)),
			),
		}},
	}, gmail.WithLogger(logger))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(parent)
	running := &runningGmailRunner{cancel: cancel, result: make(chan error, 1)}
	go func() { running.result <- runner.Run(ctx) }()
	t.Cleanup(cancel)
	return running
}

func (running *runningGmailRunner) requireRunning(t *testing.T) {
	t.Helper()
	select {
	case err := <-running.result:
		t.Fatalf("Gmail Trigger runner returned early: %v", err)
	default:
	}
}

func (running *runningGmailRunner) stop(t *testing.T) {
	t.Helper()
	running.cancel()
	select {
	case err := <-running.result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(5 * time.Second):
		t.Fatal("Gmail Trigger runner did not stop after cancellation")
	}
}

func newGmailRunnerStore(t *testing.T, endpoint string) (*localconfig.Store, string) {
	t.Helper()
	return newGmailRunnerStoreWithQueries(t, endpoint, "", "")
}

// newGmailRunnerStoreWithQueries sets each binding's Gmail search query, so a test proxy can tell the list calls apart.
func newGmailRunnerStoreWithQueries(t *testing.T, endpoint string, rootQuery string, replyQuery string) (*localconfig.Store, string) {
	t.Helper()
	bindingConfiguration := func(searchQuery string) map[string]any {
		if searchQuery == "" {
			return map[string]any{}
		}
		return map[string]any{"searchQuery": searchQuery}
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "connections.json")
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": gmail.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/google/gmail",
			"moduleVersion": "v0.8.0", "provider": "google", "connectionName": ConnectionName,
			"configuration": map[string]any{"endpoint": endpoint, "pollInterval": int64(time.Second)},
			"credentials":   map[string]any{"access_token": "SENTINEL-ACCESS-TOKEN", "primary_email": "owner@example.com"},
		}},
		"triggerBindings": []any{
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": ConnectionName, "triggerName": "messageReceived",
				"bindingName": StartTriggerBinding, "configuration": bindingConfiguration(rootQuery)},
			map[string]any{"connectorId": gmail.ConnectorID, "connectionName": ConnectionName, "triggerName": "replyReceived",
				"bindingName": ReplyTriggerBinding, "configuration": bindingConfiguration(replyQuery)},
		},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

func pendingGmailRunnerEventIDs(t *testing.T, directory string) []string {
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

type gmailInboxMessage struct {
	id       string
	threadID string
	reply    bool
}

// gmailInboxProvider serves a mutable newest-first inbox page and records sent replies by thread.
type gmailInboxProvider struct {
	*httptest.Server
	mutex         sync.Mutex
	page          []gmailInboxMessage
	listCalls     int
	sendsByThread map[string]int
	fullReadDelay map[string]time.Duration
}

func newGmailInboxProvider(t *testing.T) *gmailInboxProvider {
	t.Helper()
	provider := &gmailInboxProvider{sendsByThread: map[string]int{}, fullReadDelay: map[string]time.Duration{}}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *gmailInboxProvider) serveHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages":
		provider.mutex.Lock()
		provider.listCalls++
		references := make([]map[string]string, 0, len(provider.page))
		for _, message := range provider.page {
			references = append(references, map[string]string{"id": message.id, "threadId": message.threadID})
		}
		provider.mutex.Unlock()
		contents, err := json.Marshal(map[string]any{"messages": references})
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		writeGmailProviderResponse(response, string(contents))
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/users/me/messages/"):
		messageID := strings.TrimPrefix(request.URL.Path, "/users/me/messages/")
		message, delay, ok := provider.message(messageID)
		if !ok {
			http.NotFound(response, request)
			return
		}
		if request.URL.Query().Get("format") == "full" {
			time.Sleep(delay)
		}
		writeGmailProviderResponse(response, gmailIntegrationMessageJSON(message.id, message.threadID, message.reply))
	case request.Method == http.MethodPost && request.URL.Path == "/users/me/messages/send":
		var payload map[string]string
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(response, err.Error(), http.StatusBadRequest)
			return
		}
		provider.mutex.Lock()
		provider.sendsByThread[payload["threadId"]]++
		provider.mutex.Unlock()
		writeGmailProviderResponse(response, `{"id":"sent-`+payload["threadId"]+`","threadId":`+strconv.Quote(payload["threadId"])+`}`)
	default:
		http.NotFound(response, request)
	}
}

// receive puts messages at the top of the inbox page, as Gmail lists the newest message first.
func (provider *gmailInboxProvider) receive(messages ...gmailInboxMessage) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	newestFirst := make([]gmailInboxMessage, 0, len(messages)+len(provider.page))
	for index := len(messages) - 1; index >= 0; index-- {
		newestFirst = append(newestFirst, messages[index])
	}
	provider.page = append(newestFirst, provider.page...)
}

func (provider *gmailInboxProvider) message(messageID string) (gmailInboxMessage, time.Duration, bool) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	for _, message := range provider.page {
		if message.id == messageID {
			return message, provider.fullReadDelay[messageID], true
		}
	}
	return gmailInboxMessage{}, 0, false
}

// slowFullRead delays the Flow's full read of one message; the poller's metadata reads stay fast.
func (provider *gmailInboxProvider) slowFullRead(messageID string, delay time.Duration) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.fullReadDelay[messageID] = delay
}

// waitForCompletedPoll waits until a whole poll that started after the call has finished. Each poll lists the
// reply route, then the root route, and delivers replies last, so the next poll's first list call marks the end
// of a poll. Four list calls later, at least one whole poll has started and finished after the call.
func (provider *gmailInboxProvider) waitForCompletedPoll(t *testing.T) {
	t.Helper()
	provider.mutex.Lock()
	target := provider.listCalls + 4
	provider.mutex.Unlock()
	require.Eventually(t, func() bool {
		provider.mutex.Lock()
		defer provider.mutex.Unlock()
		return provider.listCalls >= target
	}, 15*time.Second, 50*time.Millisecond)
}

func (provider *gmailInboxProvider) sendCount(threadID string) int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.sendsByThread[threadID]
}
