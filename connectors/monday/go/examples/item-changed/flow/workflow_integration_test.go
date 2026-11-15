//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package itemchanged

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
	monday "github.com/superdurable/dex-connectors-library/connectors/monday/go"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/mondayboardfake"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "monday-item-changed", Kind: "live"}

// TestItemChangesStartOneFlowEachAcrossARestartWithRealDex: existing items are a baseline, each later change
// starts exactly one Flow, and changes made while the process is stopped arrive after a restart whose first
// request monday.com rate limits.
func TestItemChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newItemChangedEnvironment(t)
	ctx := integrationContext(t, 3*time.Minute)
	environment.provider.Create(environment.itemID(1), "Existing request")
	environment.provider.Create(environment.itemID(2), "Second existing request")
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t, environment.provider.UpdatedAt(environment.itemID(2)))

	environment.provider.Create(environment.itemID(3), "New request")
	environment.provider.SetStatus(environment.itemID(1), "Done")
	first.runner.WakePoller(ItemChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, monday.ItemChangeCreated, outcomes[environment.itemID(3)].ChangeKind)
	require.Equal(t, "New request", outcomes[environment.itemID(3)].Current.Name)
	require.Equal(t, monday.ItemChangeUpdated, outcomes[environment.itemID(1)].ChangeKind)
	status, _ := outcomes[environment.itemID(1)].Current.ColumnValueByID("status")
	require.Equal(t, "Done", status.Text, "the Flow reads the item back from monday.com")
	first.stop(t)

	environment.provider.Create(environment.itemID(4), "Filed while stopped")
	environment.provider.SetStatus(environment.itemID(3), "Stuck")
	environment.provider.RateLimitNextRequests(1, 1)
	second := environment.startTriggerProcess(t)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Filed while stopped", outcomes[environment.itemID(4)].Current.Name)
	require.Equal(t, monday.ItemChangeUpdated, outcomes[environment.itemID(3)].ChangeKind)
	environment.waitForWatermark(t, environment.provider.UpdatedAt(environment.itemID(3)))
	second.runner.WakePoller(ItemChangedTriggerBinding)
	require.Never(t, func() bool { return environment.deliveryCount() != 4 }, 2*time.Second, 100*time.Millisecond,
		"no change is delivered twice and the baseline is never delivered")
	require.True(t, wasRateLimited(environment.provider.Requests()), "the restart's first request was rate limited")
}

// TestOversizedBurstRecordsANewBaselineWithRealDex: more changes than one scan reads reseed without
// delivering the gap, and the next change starts a Flow.
func TestOversizedBurstRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newItemChangedEnvironment(t)
	ctx := integrationContext(t, 3*time.Minute)
	environment.provider.Create(environment.itemID(1), "Existing request")
	first := environment.startTriggerProcess(t)
	environment.waitForWatermark(t, environment.provider.UpdatedAt(environment.itemID(1)))
	first.stop(t)

	for index := 2; index <= 10_002; index++ {
		environment.provider.Create(environment.itemID(index), "Bulk import")
	}
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 60*time.Second, 50*time.Millisecond)
	environment.waitForWatermark(t, environment.provider.UpdatedAt(environment.itemID(10_002)))
	environment.provider.Create(environment.itemID(10_003), "After the reset")
	second.runner.WakePoller(ItemChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes[environment.itemID(10_003)].Current.Name)
	require.Equal(t, 1, environment.deliveryCount(), "items changed in the gap are not delivered")
}

func wasRateLimited(requests []mondayboardfake.Request) bool {
	for _, request := range requests {
		if request.Status == 429 {
			return true
		}
	}
	return false
}

type itemChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *mondayboardfake.Board
	connection monday.Connection
	objects    *testsupport.ObjectStore
	// itemPrefix keeps item IDs, and so Flow IDs, unique per run against a shared Dex server.
	itemPrefix    int64
	configuration monday.ItemChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[monday.ItemChangedEvent]
	order         []string
}

func newItemChangedEnvironment(t *testing.T) *itemChangedEnvironment {
	t.Helper()
	boardID := strconv.FormatInt(time.Now().UnixNano()%1_000_000_000_000+1_000_000_000_000, 10)
	fake := mondayboardfake.New(t, boardID)
	reference := sdkgo.ConnectionRef{Provider: "monday", Name: ConnectionName}
	client, err := monday.New(monday.Config{PollInterval: 30 * time.Second}, sdkgo.StaticCredentialProvider[monday.Credentials]{
		reference: {AuthMethodID: monday.PersonalAPITokenAuthMethodID, APIToken: sdkgo.NewSecretString(mondayboardfake.Token)},
	}, monday.WithAPIURL(fake.URL))
	require.NoError(t, err)
	connection, err := monday.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &itemChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, objects: testsupport.NewObjectStore(),
		itemPrefix: time.Now().UnixNano() % 1_000_000_000, configuration: monday.ItemChangedTriggerConfiguration{BoardID: boardID},
		delivered: map[string]sdkgo.TriggerEvent[monday.ItemChangedEvent]{},
	}
}

func (environment *itemChangedEnvironment) itemID(index int) string {
	return strconv.FormatInt(environment.itemPrefix*100_000+int64(index), 10)
}

func (environment *itemChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: monday.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: monday.ItemChangedTriggerDefinition.Trigger.TriggerName, BindingName: ItemChangedTriggerBinding,
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
// monday.NewProjectItemTriggerRunner does, over storage that survives the process.
func (environment *itemChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryItemChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[monday.ItemChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[monday.ItemChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[monday.ItemChangedEvent](counting))
	require.NoError(t, err)
	runner, err := monday.NewItemTriggerRunner(monday.ItemTriggerRunnerConfig{
		Connection: environment.connection,
		ItemChangedRoutes: []monday.ItemChangedTriggerRoute{{
			BindingName: ItemChangedTriggerBinding, Configuration: environment.configuration, Target: target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *itemChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *itemChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForWatermark waits until the committed cursor stands at watermark on the watched board.
func (environment *itemChangedEnvironment) waitForWatermark(t *testing.T, watermark time.Time) {
	t.Helper()
	expected := `{"boardId":"` + environment.configuration.BoardID + `","watermark":"` + watermark.UTC().Format(time.RFC3339) + `"}`
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.Token == expected
	}, 60*time.Second, 50*time.Millisecond)
}

func (environment *itemChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by item ID; a
// later change to the same item replaces the earlier outcome.
func (environment *itemChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 90*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[monday.ItemChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string]ChangeOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Item.ID, outcome.ItemID)
		outcomes[outcome.ItemID] = outcome
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
