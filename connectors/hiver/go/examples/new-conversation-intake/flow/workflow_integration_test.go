//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newconversationintake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	hiver "github.com/superdurable/dex-connectors-library/connectors/hiver/go"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go/internal/hiverfake"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TestNewConversationsStartOneFlowEachAcrossARestartWithRealDex: existing conversations are a baseline, each
// later conversation starts exactly one Flow that reads it back, and conversations that arrive while the
// process is stopped arrive after a restart whose first poll Hiver answers with 429.
func TestNewConversationsStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newIntakeEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.AddConversations(environment.inboxID, conversationIDs(573741000, 120)...)
	first := environment.startTriggerProcess(t, environment.inboxID)
	environment.waitForBaseline(t, environment.inboxID, 120)

	environment.provider.AddConversations(environment.inboxID, "573742001", "573742002")
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, hiver.ConversationStatusOpen, outcomes["573742001"].Status)
	require.Equal(t, []hiver.ConversationMessage{{HiverMessageID: "8573742002", GmailMessageID: "19cfm573742002"}}, outcomes["573742002"].Messages,
		"the Flow reads the conversation back from Hiver")
	first.stop(t)

	environment.provider.AddConversations(environment.inboxID, "573742003")
	environment.provider.RateLimitNext(time.Second)
	requestsBeforeRestart := environment.provider.ConversationListRequests()
	environment.startTriggerProcess(t, environment.inboxID)
	outcomes = environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, environment.inboxID, outcomes["573742003"].InboxID)
	require.Greater(t, environment.provider.ConversationListRequests(), requestsBeforeRestart+1, "the poll after the 429 succeeded")
	require.Equal(t, 3, environment.deliveryCount(), "no conversation is delivered twice and the baseline is never delivered")
	require.Zero(t, environment.cursorState(t).ResetCount, "a restart keeps the baseline")
}

// TestRepointedBindingRecordsANewBaselineWithRealDex: a cursor for another inbox is invalid, so the
// binding records the new inbox's conversations as a baseline and delivers only later ones.
func TestRepointedBindingRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newIntakeEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.AddConversations(environment.inboxID, "573741001")
	first := environment.startTriggerProcess(t, environment.inboxID)
	environment.waitForBaseline(t, environment.inboxID, 1)
	first.stop(t)

	otherInboxID := environment.inboxID + "7"
	environment.provider.AddInbox(otherInboxID, "billing@acme.example.com")
	environment.provider.AddConversations(otherInboxID, "573751001", "573751002")
	environment.startTriggerProcess(t, otherInboxID)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForBaseline(t, otherInboxID, 2)
	environment.provider.AddConversations(otherInboxID, "573752001")
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, otherInboxID, outcomes["573752001"].InboxID)
	require.NotContains(t, outcomes, "573751001", "conversations present at the new baseline are not delivered")
}

type intakeEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *hiverfake.Server
	connection hiver.Connection
	store      *projectstoretest.Store
	// inboxID is unique per run, which keeps Flow IDs unique on a shared Dex Server.
	inboxID   string
	mutex     sync.Mutex
	delivered map[string]sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]
	order     []string
}

func newIntakeEnvironment(t *testing.T) *intakeEnvironment {
	t.Helper()
	inboxID := fmt.Sprint(time.Now().UnixNano())
	fake := hiverfake.New(inboxID, "support@acme.example.com")
	t.Cleanup(fake.Close)
	reference := sdkgo.ConnectionRef{Provider: "hiver", Name: ConnectionName}
	client, err := hiver.New(hiver.Config{PollInterval: time.Second, RequestIntervalMilliseconds: 1}, sdkgo.StaticCredentialProvider[hiver.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(hiverfake.APIKey)},
	}, hiver.WithAPIBaseURL(fake.URL))
	require.NoError(t, err)
	connection, err := hiver.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &intakeEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, store: projectstoretest.New(),
		inboxID: inboxID, delivered: map[string]sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]{},
	}
}

func (environment *intakeEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: hiver.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: hiver.ConversationReceivedTriggerDefinition.Trigger.TriggerName, BindingName: ConversationReceivedTriggerBinding,
	}
}

// triggerProcess is one run of the application's Trigger side; stopping it models a process exit.
type triggerProcess struct {
	cancel   context.CancelFunc
	result   chan error
	stopOnce sync.Once
	stopErr  error
}

func (process *triggerProcess) stop(t *testing.T) {
	t.Helper()
	require.ErrorIs(t, process.stopAndWait(), context.Canceled)
}

func (process *triggerProcess) stopAndWait() error {
	process.stopOnce.Do(func() {
		process.cancel()
		process.stopErr = <-process.result
	})
	return process.stopErr
}

// startTriggerProcess wires the example's target behind the durable inbox and project cursor, as
// hiver.NewProjectConversationReceivedTriggerRunner does, over storage that survives the process.
func (environment *intakeEnvironment) startTriggerProcess(t *testing.T, inboxID string) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryConversation, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[hiver.ConversationReceivedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[hiver.ConversationReceivedEvent]) error {
		if err := start.HandleTrigger(ctx, event); err != nil {
			return err
		}
		environment.mutex.Lock()
		defer environment.mutex.Unlock()
		if _, ok := environment.delivered[event.ID]; !ok {
			environment.order = append(environment.order, event.ID)
		}
		environment.delivered[event.ID] = event
		return nil
	})
	key := environment.inboxKey()
	inbox, err := environment.store.Inbox(key)
	require.NoError(t, err)
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[hiver.ConversationReceivedEvent](counting))
	require.NoError(t, err)
	cursor, err := environment.store.Cursor(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	runner, err := hiver.NewConversationReceivedTriggerRunner(hiver.ConversationReceivedTriggerRunnerConfig{
		Connection: environment.connection,
		Routes: []hiver.ConversationReceivedTriggerRoute{{
			BindingName: ConversationReceivedTriggerBinding, Configuration: hiver.ConversationReceivedTriggerConfiguration{InboxID: inboxID},
			Cursor: cursor, Target: target,
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *intakeEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	cursor, err := environment.store.Cursor(projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	record, err := cursor.ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForBaseline waits until the committed cursor finished the baseline of inboxID with tracked entries.
func (environment *intakeEnvironment) waitForBaseline(t *testing.T, inboxID string, tracked int) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		if state == nil || state.Snapshot == nil {
			return false
		}
		var token struct {
			InboxID            string `json:"inboxId"`
			IsBaselineComplete bool   `json:"baselineComplete"`
		}
		return json.Unmarshal([]byte(state.Token), &token) == nil && token.InboxID == inboxID && token.IsBaselineComplete &&
			len(state.Snapshot.Entries) == tracked
	}, 30*time.Second, 50*time.Millisecond)
	require.Zero(t, environment.deliveryCount(), "the baseline delivers nothing")
}

func (environment *intakeEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered conversations, then for each one's Flow, keyed by conversation ID.
func (environment *intakeEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]IntakeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[hiver.ConversationReceivedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string]IntakeOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome IntakeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Conversation.ID, outcome.ConversationID)
		outcomes[outcome.ConversationID] = outcome
	}
	return outcomes
}

func conversationIDs(first int, n int) []string {
	ids := make([]string, n)
	for index := range ids {
		ids[index] = fmt.Sprint(first + index)
	}
	return ids
}

type dexHarness struct {
	worker       *dex.Worker
	workerResult chan error
	client       *dex.Client
	cache        *blobcache.Cache
}

func newDexHarness(t *testing.T, flow *Flow) *dexHarness {
	t.Helper()
	registry, err := dex.NewRegistry([]dex.Flow{flow})
	require.NoError(t, err)
	cache, err := blobcache.New(&blobcache.Config{Dir: filepath.Join(t.TempDir(), "blobs"), MaxBytes: 64 << 20})
	require.NoError(t, err)
	address := os.Getenv("DEX_FLOW_SERVICE_ADDRESS")
	if address == "" {
		address = "127.0.0.1:8801"
	}
	harness := &dexHarness{cache: cache, workerResult: make(chan error, 1)}
	harness.client, err = dex.NewClient(registry, cache, dex.ClientOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	harness.worker, err = dex.NewWorker(registry, cache, dex.WorkerOptions{FlowServiceAddress: address})
	require.NoError(t, err)
	go func() { harness.workerResult <- harness.worker.Start() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, errors.Join(harness.worker.Stop(ctx), <-harness.workerResult, harness.client.Close(), harness.cache.Close()))
	})
	registrationContext, cancelRegistration := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancelRegistration()
	require.NoError(t, harness.worker.WaitPullRegistered(registrationContext))
	return harness
}

func integrationContext(t *testing.T, timeout time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	t.Cleanup(cancel)
	return ctx
}
