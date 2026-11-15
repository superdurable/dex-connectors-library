//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package rowchanged

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	notion "github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go/internal/notionrowsfake"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "notion-row-changed", Kind: "live"}

const (
	rowStandup = "2a4d6f80-1b3c-4d5e-8f70-0000000000a1"
	rowReview  = "2a4d6f80-1b3c-4d5e-8f70-0000000000b2"
	rowKickoff = "2a4d6f80-1b3c-4d5e-8f70-0000000000c3"
	rowOffline = "2a4d6f80-1b3c-4d5e-8f70-0000000000d4"
)

// TestRowChangesStartOneFlowEachAcrossARestartWithRealDex: existing rows are a baseline, each later row
// version starts exactly one Flow, and edits made while the process is stopped arrive after a restart.
func TestRowChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newRowChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Create(rowStandup, "Standup", environment.now.Add(-time.Hour))
	environment.provider.Create(rowReview, "Review", environment.now.Add(-30*time.Minute))
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedHighWater(t, environment.now.Add(-30*time.Minute))

	environment.provider.Create(rowKickoff, "Kickoff", environment.now)
	environment.provider.Edit(rowStandup, "Standup moved", environment.now)
	environment.provider.Trash(rowReview, environment.now)
	first.runner.WakePoller(DatabaseRowChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, notion.DatabaseRowCreated, outcomes["Kickoff"].ChangeKind)
	require.Equal(t, notion.DatabaseRowUpdated, outcomes["Standup moved"].ChangeKind)
	require.Equal(t, rowStandup, outcomes["Standup moved"].Current.ID, "the Flow reads the row back from Notion")
	first.stop(t)

	environment.provider.Create(rowOffline, "Planned while stopped", environment.now.Add(time.Minute))
	environment.provider.Edit(rowKickoff, "Kickoff moved", environment.now.Add(time.Minute))
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(DatabaseRowChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, notion.DatabaseRowCreated, outcomes["Planned while stopped"].ChangeKind)
	require.Equal(t, notion.DatabaseRowUpdated, outcomes["Kickoff moved"].ChangeKind)
	environment.waitForCommittedHighWater(t, environment.now.Add(time.Minute))
	second.runner.WakePoller(DatabaseRowChangedTriggerBinding)
	environment.waitForQueries(t, 1)
	require.Equal(t, 4, environment.deliveryCount(), "no row version is delivered twice, the baseline and the trashed row never")
}

// TestRateLimitAndForeignCursorRecordANewBaselineWithRealDex: a 429 pauses the poll for Retry-After, and a
// stored cursor for another data source, as after the binding was re-picked, reseeds without a backlog.
func TestRateLimitAndForeignCursorRecordANewBaselineWithRealDex(t *testing.T) {
	environment := newRowChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Create(rowStandup, "Existing", environment.now.Add(-time.Hour))
	foreign := `{"v":1,"dataSourceId":"2a4d6f80-1b3c-4d5e-8f70-999999999999","highWater":"2026-01-01T00:00:00Z"}`
	store := environment.cursorStore(t)
	record, err := store.ReadPollCursor(context.Background())
	require.NoError(t, err)
	_, err = store.ReplacePollCursor(context.Background(), record.Revision, projectconfig.PollCursorState{Token: foreign})
	require.NoError(t, err)
	environment.provider.RateLimitNextRequests(1, "1")
	process := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForCommittedHighWater(t, environment.now.Add(-time.Hour))
	require.Equal(t, 429, environment.provider.Requests()[0].Status)

	environment.provider.Create(rowKickoff, "After the reset", environment.now)
	process.runner.WakePoller(DatabaseRowChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, notion.DatabaseRowCreated, outcomes["After the reset"].ChangeKind)
	require.NotContains(t, outcomes, "Existing", "the new baseline delivers no existing row")
}

type rowChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *notionrowsfake.DataSource
	connection notion.Connection
	objects    *testsupport.ObjectStore
	now        time.Time
	// configuration is the saved picker value; a unique data source keeps Flow IDs unique per run.
	configuration notion.DatabaseRowChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]
	order         []string
}

func newRowChangedEnvironment(t *testing.T) *rowChangedEnvironment {
	t.Helper()
	dataSourceID := uniqueNotionID(t)
	fake := notionrowsfake.New(t, dataSourceID, uniqueNotionID(t))
	reference := sdkgo.ConnectionRef{Provider: "notion", Name: ConnectionName}
	client, err := notion.New(notion.Config{Endpoint: fake.URL, PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[notion.Credentials]{
		reference: {APIToken: sdkgo.NewSecretString(notionrowsfake.Token)},
	})
	require.NoError(t, err)
	connection, err := notion.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &rowChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), now: time.Now().UTC().Truncate(time.Minute),
		configuration: notion.DatabaseRowChangedTriggerConfiguration{DataSourceID: dataSourceID, DataSourceName: "Requests"},
		delivered:     map[string]sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]{},
	}
}

func uniqueNotionID(t *testing.T) string {
	t.Helper()
	random := make([]byte, 16)
	_, err := rand.Read(random)
	require.NoError(t, err)
	text := hex.EncodeToString(random)
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}

func (environment *rowChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: notion.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: notion.DatabaseRowChangedTriggerDefinition.Trigger.TriggerName, BindingName: DatabaseRowChangedTriggerBinding,
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
// notion.NewProjectDatabaseRowTriggerRunner does, over storage that survives the process.
func (environment *rowChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryRowChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[notion.DatabaseRowChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[notion.DatabaseRowChangedEvent](counting))
	require.NoError(t, err)
	runner, err := notion.NewDatabaseRowTriggerRunner(notion.DatabaseRowTriggerRunnerConfig{
		Connection: environment.connection,
		DatabaseRowChangedRoutes: []notion.DatabaseRowChangedTriggerRoute{{
			BindingName: DatabaseRowChangedTriggerBinding, Configuration: environment.configuration,
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

func (environment *rowChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *rowChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCommittedHighWater waits until the committed cursor watches this data source up to highWater.
func (environment *rowChangedEnvironment) waitForCommittedHighWater(t *testing.T, highWater time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		var position struct {
			DataSourceID string    `json:"dataSourceId"`
			HighWater    time.Time `json:"highWater"`
			StartCursor  string    `json:"startCursor"`
		}
		return state != nil && json.Unmarshal([]byte(state.Token), &position) == nil &&
			position.DataSourceID == environment.configuration.DataSourceID && position.HighWater.Equal(highWater) && position.StartCursor == ""
	}, 30*time.Second, 50*time.Millisecond)
}

// waitForQueries waits for count more data source queries than were recorded when it was called.
func (environment *rowChangedEnvironment) waitForQueries(t *testing.T, count int) {
	t.Helper()
	baseline := len(environment.provider.Requests())
	require.Eventually(t, func() bool { return len(environment.provider.Requests()) >= baseline+count }, 30*time.Second, 50*time.Millisecond)
}

func (environment *rowChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by the title the
// row had when it was read back.
func (environment *rowChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[notion.DatabaseRowChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Row.ID, outcome.RowID)
		outcomes[event.Payload.Row.Title] = outcome
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
