//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package taskchanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	asana "github.com/superdurable/dex-connectors-library/connectors/asana/go"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go/internal/asanaeventsfake"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "asana-task-changed", Kind: "live"}

// TestTaskChangesStartOneFlowEachAcrossARestartWithRealDex: existing tasks are a baseline, each later task
// event starts exactly one Flow, story events start none, and events made while the process is stopped
// arrive after a restart whose first poll is rate limited.
func TestTaskChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newTaskChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.AddTask("1204000000000001", "Inspect door sensors")
	environment.provider.AddTask("1204000000000002", "Order spare badges")
	first := environment.startTriggerProcess(t)
	environment.waitForSyncToken(t, environment.provider.SyncToken())

	environment.provider.AddTask("1204000000000003", "Replace badge reader")
	environment.provider.CompleteTask("1204000000000001")
	environment.provider.AddStory("1205000000000001")
	environment.provider.DeleteTask("1204000000000002")
	first.runner.WakePoller(TaskChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, asana.TaskChangeAdded, outcomes[0].Action)
	require.Equal(t, "Replace badge reader", outcomes[0].Current.Name, "the Flow reads the task back from Asana")
	require.Equal(t, asana.TaskChangeChanged, outcomes[1].Action)
	require.Equal(t, "completed", outcomes[1].ChangedField)
	require.True(t, outcomes[1].Current.IsCompleted)
	require.Equal(t, asana.TaskChangeDeleted, outcomes[2].Action)
	require.Nil(t, outcomes[2].Current, "a deleted task is not read back")
	first.stop(t)

	environment.provider.AddTask("1204000000000004", "Planned while stopped")
	environment.provider.RenameTask("1204000000000003", "Replace badge reader at door 4")
	environment.provider.RateLimitNext(1, "1")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TaskChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 5)
	require.Equal(t, "Planned while stopped", outcomes[3].Current.Name)
	require.Equal(t, "name", outcomes[4].ChangedField)
	require.Equal(t, "Replace badge reader at door 4", outcomes[4].Current.Name)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	require.Equal(t, 5, environment.deliveryCount(), "no event is delivered twice and the baseline is never delivered")
	require.True(t, environment.answeredRateLimit(), "the restarted poller met a 429 and waited")
}

// TestExpiredSyncTokenRecordsANewBaselineWithRealDex: a 412 reseeds without delivering the gap.
func TestExpiredSyncTokenRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newTaskChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.AddTask("1204000000000001", "Inspect door sensors")
	first := environment.startTriggerProcess(t)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	first.stop(t)

	environment.provider.AddTask("1204000000000009", "Lost in the gap")
	environment.provider.ExpireSyncTokens()
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	environment.provider.AddTask("1204000000000010", "After the reset")
	second.runner.WakePoller(TaskChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Len(t, outcomes, 1, "events between the expired token and the new baseline are not delivered")
	require.Equal(t, "After the reset", outcomes[0].Current.Name)
}

type taskChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *asanaeventsfake.Project
	connection asana.Connection
	objects    *testsupport.ObjectStore
	// projectConfiguration is the saved picker value; a unique project keeps Flow IDs unique per run.
	projectConfiguration asana.TaskChangedTriggerConfiguration
	mutex                sync.Mutex
	delivered            map[string]sdkgo.TriggerEvent[asana.TaskChangedEvent]
	order                []string
}

func newTaskChangedEnvironment(t *testing.T) *taskChangedEnvironment {
	t.Helper()
	projectID := strconv.FormatInt(time.Now().UnixNano(), 10)
	fake := asanaeventsfake.New(t, projectID)
	reference := sdkgo.ConnectionRef{Provider: "asana", Name: ConnectionName}
	client, err := asana.New(asana.Config{Endpoint: fake.URL, PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[asana.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(asanaeventsfake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := asana.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &taskChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects:              testsupport.NewObjectStore(),
		projectConfiguration: asana.TaskChangedTriggerConfiguration{ProjectID: projectID, ProjectName: "Facilities"},
		delivered:            map[string]sdkgo.TriggerEvent[asana.TaskChangedEvent]{},
	}
}

func (environment *taskChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: asana.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: asana.TaskChangedTriggerDefinition.Trigger.TriggerName, BindingName: TaskChangedTriggerBinding,
	}
}

func (environment *taskChangedEnvironment) answeredRateLimit() bool {
	for _, request := range environment.provider.Requests() {
		if request.Status == 429 {
			return true
		}
	}
	return false
}

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
// asana.NewProjectTaskTriggerRunner does, over storage that survives the process.
func (environment *taskChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryTaskChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[asana.TaskChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[asana.TaskChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[asana.TaskChangedEvent](counting))
	require.NoError(t, err)
	runner, err := asana.NewTaskTriggerRunner(asana.TaskTriggerRunnerConfig{
		Connection: environment.connection,
		TaskChangedRoutes: []asana.TaskChangedTriggerRoute{{
			BindingName: TaskChangedTriggerBinding, Configuration: environment.projectConfiguration,
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

func (environment *taskChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *taskChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForSyncToken waits until the committed cursor holds syncToken and no pending page.
func (environment *taskChangedEnvironment) waitForSyncToken(t *testing.T, syncToken string) {
	t.Helper()
	expected := `{"projectId":"` + environment.projectConfiguration.ProjectID + `","sync":"` + syncToken + `"}`
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.Token == expected
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *taskChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered task events, then returns each event's Flow outcome in
// delivery order.
func (environment *taskChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) []ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[asana.TaskChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := make([]ChangeOutcome, 0, len(events))
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Task.ID, outcome.TaskID)
		outcomes = append(outcomes, outcome)
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
