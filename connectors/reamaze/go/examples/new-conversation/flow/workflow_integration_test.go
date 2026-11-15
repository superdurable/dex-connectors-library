//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newconversation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go/internal/reamazefake"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationEmail = "agent@acme.example.com"
	integrationToken = "reamazeIntegrationToken0123"
)

var integrationScope = projectconfig.Scope{ProjectID: "reamaze-new-conversation", Kind: "live"}

// TestNewConversationsStartOneFlowEachAcrossARestartWithRealDex: existing conversations are a baseline,
// each later non-spam conversation starts exactly one Flow, a 429 pauses and resumes the poll, and
// conversations created while the process is stopped arrive after a restart.
func TestNewConversationsStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newConversationEnvironment(t)
	ctx := integrationContext(t, 3*time.Minute)
	environment.insert("existing-1", "Existing", 0, -2*time.Hour)
	environment.insert("existing-2", "Existing", 0, -time.Hour)
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t, -time.Hour)

	environment.insert("refund", "Refund request", reamaze.ConversationStatusOpen, 0)
	environment.insert("spam", "Cheap watches", reamaze.ConversationStatusSpam, time.Second)
	environment.insert("closed", "Closed on arrival", reamaze.ConversationStatusArchived, 2*time.Second)
	environment.provider.RateLimitNextList("1")
	first.runner.WakePoller(ConversationCreatedTriggerBinding)
	intakes := environment.waitForIntakes(ctx, t, 3, []string{"refund", "closed"})
	require.Equal(t, "Refund request", intakes["refund"].Subject)
	require.Equal(t, 1, intakes["refund"].MessageCount, "the Flow reads the conversation back from Re:amaze")
	require.Equal(t, reamaze.ConversationStatusArchived, intakes["closed"].Status)
	require.GreaterOrEqual(t, environment.provider.ListRequests(), 3, "the rate-limited poll was retried")
	first.stop(t)

	environment.insert("offline", "Opened while stopped", reamaze.ConversationStatusOpen, 3*time.Second)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(ConversationCreatedTriggerBinding)
	intakes = environment.waitForIntakes(ctx, t, 4, []string{"refund", "closed", "offline"})
	require.Equal(t, "Opened while stopped", intakes["offline"].Subject)
	environment.waitForWatermark(t, 3*time.Second)
	require.Equal(t, 4, environment.deliveryCount(), "spam is delivered once and starts no Flow; nothing else repeats")
}

// TestChangedChannelRecordsANewBaselineWithRealDex: a cursor for another channel reseeds without
// delivering the conversations created before the new baseline.
func TestChangedChannelRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newConversationEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.insert("existing", "Existing", 0, -time.Hour)
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t, -time.Hour)
	first.stop(t)

	environment.insertInChannel("gap", "Lost in the gap", "billing", 0)
	environment.configuration = reamaze.ConversationCreatedTriggerConfiguration{Channel: "billing"}
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.insertInChannel("after-reset", "After the reset", "billing", time.Second)
	second.runner.WakePoller(ConversationCreatedTriggerBinding)

	intakes := environment.waitForIntakes(ctx, t, 1, []string{"after-reset"})
	require.Equal(t, "billing", intakes["after-reset"].Channel)
	require.Equal(t, 1, environment.deliveryCount(), "the conversation before the new baseline is not delivered")
}

type conversationEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	provider      *reamazefake.Provider
	connection    reamaze.Connection
	objects       *testsupport.ObjectStore
	configuration reamaze.ConversationCreatedTriggerConfiguration
	// prefix keeps slugs, and so Flow IDs, unique per run against a shared Dex Server.
	prefix    string
	start     time.Time
	mutex     sync.Mutex
	delivered map[string]sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]
}

func newConversationEnvironment(t *testing.T) *conversationEnvironment {
	t.Helper()
	fake := reamazefake.New(t, integrationEmail, integrationToken)
	reference := sdkgo.ConnectionRef{Provider: "reamaze", Name: ConnectionName}
	client, err := reamaze.New(reamaze.Config{Brand: "acme", PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[reamaze.Credentials]{
		reference: {Email: integrationEmail, APIToken: sdkgo.NewSecretString(integrationToken)},
	}, reamaze.WithAPIBaseURL(fake.URL))
	require.NoError(t, err)
	connection, err := reamaze.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &conversationEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, objects: testsupport.NewObjectStore(),
		prefix: "run" + strconv.FormatInt(time.Now().UnixNano(), 36), start: time.Now().UTC().Truncate(time.Millisecond),
		delivered: map[string]sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]{},
	}
}

func (environment *conversationEnvironment) insert(name string, subject string, status reamaze.ConversationStatus, offset time.Duration) {
	environment.provider.Insert(reamazefake.Conversation{
		Slug: environment.prefix + "-" + name, Subject: subject, Status: int(status), CreatedAt: environment.start.Add(offset),
	})
}

func (environment *conversationEnvironment) insertInChannel(name string, subject string, channel string, offset time.Duration) {
	environment.provider.Insert(reamazefake.Conversation{
		Slug: environment.prefix + "-" + name, Subject: subject, Channel: channel, CreatedAt: environment.start.Add(offset),
	})
}

func (environment *conversationEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: reamaze.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: reamaze.ConversationCreatedTriggerDefinition.Trigger.TriggerName, BindingName: ConversationCreatedTriggerBinding,
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
// reamaze.NewProjectConversationTriggerRunner does, over storage that survives the process.
func (environment *conversationEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptCustomerConversation, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[reamaze.ConversationCreatedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[reamaze.ConversationCreatedEvent]) error {
		if err := start.HandleTrigger(ctx, event); err != nil {
			return err
		}
		environment.mutex.Lock()
		defer environment.mutex.Unlock()
		environment.delivered[event.Payload.Conversation.ID] = event
		return nil
	})
	key := environment.inboxKey()
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, integrationScope, key)
	require.NoError(t, err)
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[reamaze.ConversationCreatedEvent](counting))
	require.NoError(t, err)
	runner, err := reamaze.NewConversationTriggerRunner(reamaze.ConversationTriggerRunnerConfig{
		Connection: environment.connection,
		ConversationCreatedRoutes: []reamaze.ConversationCreatedTriggerRoute{{
			BindingName: ConversationCreatedTriggerBinding, Configuration: environment.configuration,
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

func (environment *conversationEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *conversationEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForWatermark waits until the committed cursor's newest creation time is start plus offset.
func (environment *conversationEnvironment) waitForWatermark(t *testing.T, offset time.Duration) {
	t.Helper()
	expected := `"watermark":` + strconv.FormatInt(environment.start.Add(offset).UnixMilli(), 10) + `,`
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && strings.Contains(state.Token, expected)
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *conversationEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForIntakes waits for count delivered conversations, then for the Flow of each named one.
func (environment *conversationEnvironment) waitForIntakes(ctx context.Context, t *testing.T, count int, names []string) map[string]Intake {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 60*time.Second, 50*time.Millisecond)
	intakes := map[string]Intake{}
	for _, name := range names {
		environment.mutex.Lock()
		event, ok := environment.delivered[environment.prefix+"-"+name]
		environment.mutex.Unlock()
		require.True(t, ok, "%s was not delivered", name)
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var intake Intake
		require.NoError(t, result.DecodeSingleOutput(&intake))
		require.Equal(t, event.Payload.Conversation.ID, intake.ConversationID)
		intakes[name] = intake
	}
	return intakes
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
