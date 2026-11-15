//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messagereceived

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	messaging "github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/internal/twiliofake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// Split literals keep synthetic SIDs from matching repository secret scanning.
	integrationAccountSID = "AC" + "0123456789abcdef0123456789abcdef"
	integrationAuthToken  = "integration-auth-token-0123456789"
	supportNumber         = "+14155550100"
	salesNumber           = "+14155550199"
	customerNumber        = "+14155550123"
)

var integrationScope = projectconfig.Scope{ProjectID: "twilio-message-received", Kind: "live"}

// TestInboundMessagesStartOneFlowEachAcrossARestartWithRealDex: existing messages are a baseline, each
// later inbound message to the number starts exactly one Flow, outbound and other-number messages start
// none, and messages received while the process is stopped arrive after a restart.
func TestInboundMessagesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newMessageReceivedEnvironment(t, supportNumber)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Receive(customerNumber, supportNumber, "before the baseline", time.Now().Add(-30*time.Second), "")
	first := environment.startTriggerProcess(t)
	environment.waitForSeed(t)

	hello := environment.provider.Receive(customerNumber, supportNumber, "hello", time.Now(), "")
	stop := environment.provider.Receive(customerNumber, supportNumber, "STOP", time.Now(), "")
	environment.provider.Send(supportNumber, customerNumber, "outbound reply", time.Now())
	environment.provider.Receive(customerNumber, salesNumber, "to sales", time.Now(), "")
	first.runner.WakePoller(MessageReceivedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, messaging.MessageStatusReceived, outcomes[hello].Current.Status, "the Flow reads the message back from Twilio")
	require.Equal(t, customerNumber, outcomes[hello].From)
	require.False(t, outcomes[hello].IsOptOut)
	require.True(t, outcomes[stop].IsOptOut)
	first.stop(t)

	offline := environment.provider.Receive(customerNumber, supportNumber, "sent while stopped", time.Now(), "")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(MessageReceivedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, supportNumber, outcomes[offline].To)
	second.runner.WakePoller(MessageReceivedTriggerBinding)
	environment.waitForQuietPoll(t)
	require.Equal(t, 3, environment.deliveryCount(), "no message is delivered twice and the baseline is never delivered")
}

// TestRateLimitPausesAndAProcessingMMSWaitsWithRealDex: a 429 pauses the poll for Retry-After, and an
// inbound MMS still in receiving starts its Flow only once Twilio reports received.
func TestRateLimitPausesAndAProcessingMMSWaitsWithRealDex(t *testing.T) {
	environment := newMessageReceivedEnvironment(t, supportNumber)
	ctx := integrationContext(t, 2*time.Minute)
	process := environment.startTriggerProcess(t)
	environment.waitForSeed(t)

	photo := environment.provider.Receive(customerNumber, supportNumber, "photo", time.Now(), string(messaging.MessageStatusReceiving))
	environment.provider.ThrottleNextList(time.Second)
	requestsBefore := len(environment.provider.ListRequests())
	process.runner.WakePoller(MessageReceivedTriggerBinding)
	require.Eventually(t, func() bool { return len(environment.provider.ListRequests()) >= requestsBefore+2 }, 30*time.Second, 50*time.Millisecond,
		"the poll after the 429 runs once the pause ends")
	require.Zero(t, environment.deliveryCount(), "a message still in receiving is held")

	environment.provider.SetStatus(photo, string(messaging.MessageStatusReceived))
	process.runner.WakePoller(MessageReceivedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, messaging.MessageStatusReceived, outcomes[photo].Current.Status)
}

// TestChangedReceivingNumberRecordsANewBaselineWithRealDex: a binding saved for another number reseeds
// without delivering the old number's messages or the gap.
func TestChangedReceivingNumberRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newMessageReceivedEnvironment(t, supportNumber)
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForSeed(t)
	first.stop(t)

	environment.provider.Receive(customerNumber, salesNumber, "before the new baseline", time.Now(), "")
	environment.configuration = messaging.MessageReceivedTriggerConfiguration{To: salesNumber}
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	after := environment.provider.Receive(customerNumber, salesNumber, "after the reset", time.Now(), "")
	second.runner.WakePoller(MessageReceivedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, salesNumber, outcomes[after].To)
	require.Len(t, outcomes, 1, "messages before the new baseline are not delivered")
}

type messageReceivedEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	provider      *twiliofake.Messages
	connection    messaging.Connection
	objects       *testsupport.ObjectStore
	configuration messaging.MessageReceivedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[messaging.MessageReceivedEvent]
	order         []string
}

func newMessageReceivedEnvironment(t *testing.T, receivingNumber string) *messageReceivedEnvironment {
	t.Helper()
	fake := twiliofake.New(t, integrationAccountSID, integrationAccountSID, integrationAuthToken)
	reference := sdkgo.ConnectionRef{Provider: "twilio", Name: ConnectionName}
	client, err := messaging.New(messaging.Config{AccountSID: integrationAccountSID, Endpoint: fake.URL, PollInterval: 5 * time.Second},
		sdkgo.StaticCredentialProvider[messaging.Credentials]{
			reference: {AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(integrationAuthToken)},
		})
	require.NoError(t, err)
	connection, err := messaging.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &messageReceivedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, objects: testsupport.NewObjectStore(),
		configuration: messaging.MessageReceivedTriggerConfiguration{To: receivingNumber},
		delivered:     map[string]sdkgo.TriggerEvent[messaging.MessageReceivedEvent]{},
	}
}

func (environment *messageReceivedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: messaging.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: messaging.MessageReceivedTriggerDefinition.Trigger.TriggerName, BindingName: MessageReceivedTriggerBinding,
	}
}

// triggerProcess is one run of the application's Trigger side; stopping it models a process exit.
type triggerProcess struct {
	runner   *pollingtrigger.Runner
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
// messaging.NewProjectMessageTriggerRunner does, over storage that survives the process.
func (environment *messageReceivedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryInboundMessage, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[messaging.MessageReceivedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[messaging.MessageReceivedEvent]) error {
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
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, integrationScope, key)
	require.NoError(t, err)
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[messaging.MessageReceivedEvent](counting))
	require.NoError(t, err)
	runner, err := messaging.NewMessageTriggerRunner(messaging.MessageTriggerRunnerConfig{
		Connection: environment.connection,
		MessageReceivedRoutes: []messaging.MessageReceivedTriggerRoute{{
			BindingName: MessageReceivedTriggerBinding, Configuration: environment.configuration,
			Target: target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *messageReceivedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *messageReceivedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

func (environment *messageReceivedEnvironment) waitForSeed(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
}

// waitForQuietPoll waits until two more list polls ran, so the first finished every delivery it started.
func (environment *messageReceivedEnvironment) waitForQuietPoll(t *testing.T) {
	t.Helper()
	before := len(environment.provider.ListRequests())
	require.Eventually(t, func() bool { return len(environment.provider.ListRequests()) > before }, 30*time.Second, 50*time.Millisecond)
	before = len(environment.provider.ListRequests())
	require.Eventually(t, func() bool { return len(environment.provider.ListRequests()) > before }, 30*time.Second, 50*time.Millisecond)
}

func (environment *messageReceivedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered messages, then for each message's Flow, keyed by message SID.
func (environment *messageReceivedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]InboundMessageOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[messaging.MessageReceivedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string]InboundMessageOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome InboundMessageOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Message.SID, outcome.MessageSID)
		outcomes[outcome.MessageSID] = outcome
	}
	return outcomes
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
