//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package eventchanged

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
	calendar "github.com/superdurable/dex-connectors-library/connectors/google/calendar/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/go/internal/calendarsyncfake"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "google-calendar-event-changed", Kind: "live"}

// TestEventChangesStartOneFlowEachAcrossARestartWithRealDex: existing events are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestEventChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newEventChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Insert("standup0001", "Standup")
	environment.provider.Insert("review00001", "Review")
	first := environment.startTriggerProcess(t)
	environment.waitForSyncToken(t, environment.provider.SyncToken())

	environment.provider.Insert("kickoff0001", "Kickoff")
	environment.provider.Update("standup0001", "Standup moved")
	environment.provider.Cancel("review00001")
	first.runner.WakePoller(EventChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, calendar.EventChangeCreated, outcomes["kickoff0001"].ChangeKind)
	require.Equal(t, "Kickoff", outcomes["kickoff0001"].Current.Summary)
	require.Equal(t, calendar.EventChangeUpdated, outcomes["standup0001"].ChangeKind)
	require.Equal(t, "Standup moved", outcomes["standup0001"].Current.Summary, "the Flow reads the event back from Google")
	require.Equal(t, calendar.EventChangeCancelled, outcomes["review00001"].ChangeKind)
	require.Nil(t, outcomes["review00001"].Current, "a cancelled event is not read back")
	first.stop(t)

	environment.provider.Insert("offline0001", "Planned while stopped")
	environment.provider.Update("kickoff0001", "Kickoff moved")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(EventChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 5)
	require.Equal(t, "Planned while stopped", outcomes["offline0001"].Current.Summary)
	require.Equal(t, calendar.EventChangeUpdated, outcomes["kickoff0001"].ChangeKind)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	require.Equal(t, 5, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
}

// TestExpiredSyncTokenRecordsANewBaselineWithRealDex: a 410 Gone reseeds without delivering the gap.
func TestExpiredSyncTokenRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newEventChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Insert("standup0001", "Standup")
	first := environment.startTriggerProcess(t)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	first.stop(t)

	environment.provider.Insert("gapevent001", "Lost in the gap")
	environment.provider.ExpireSyncTokens()
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForSyncToken(t, environment.provider.SyncToken())
	environment.provider.Insert("afterreset1", "After the reset")
	second.runner.WakePoller(EventChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes["afterreset1"].Current.Summary)
	require.NotContains(t, outcomes, "gapevent001", "changes between the expired token and the new baseline are not delivered")
}

type eventChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *calendarsyncfake.Calendar
	connection calendar.Connection
	objects    *testsupport.ObjectStore
	// calendarConfiguration is the saved picker value; a unique calendar keeps Flow IDs unique per run.
	calendarConfiguration calendar.EventChangedTriggerConfiguration
	mutex                 sync.Mutex
	delivered             map[string]sdkgo.TriggerEvent[calendar.EventChangedEvent]
	order                 []string
}

func newEventChangedEnvironment(t *testing.T) *eventChangedEnvironment {
	t.Helper()
	calendarID := "team-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@group.calendar.google.com"
	fake := calendarsyncfake.New(t, calendarID)
	reference := sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}
	client, err := calendar.New(calendar.Config{Endpoint: fake.URL, PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[calendar.Credentials]{
		reference: {AuthMethodID: calendar.GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	})
	require.NoError(t, err)
	connection, err := calendar.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	environment := &eventChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[calendar.EventChangedEvent]{},
	}
	environment.calendarConfiguration = calendar.EventChangedTriggerConfiguration{CalendarID: calendarID}
	return environment
}

func (environment *eventChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: calendar.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: calendar.EventChangedTriggerDefinition.Trigger.TriggerName, BindingName: EventChangedTriggerBinding,
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
// calendar.NewProjectEventTriggerRunner does, over storage that survives the process.
func (environment *eventChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryEventChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[calendar.EventChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[calendar.EventChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[calendar.EventChangedEvent](counting))
	require.NoError(t, err)
	runner, err := calendar.NewEventTriggerRunner(calendar.EventTriggerRunnerConfig{
		Connection: environment.connection,
		EventChangedRoutes: []calendar.EventChangedTriggerRoute{{
			BindingName: EventChangedTriggerBinding, Configuration: environment.calendarConfiguration,
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

func (environment *eventChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *eventChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForSyncToken waits until the committed cursor holds syncToken and no pending page.
func (environment *eventChangedEnvironment) waitForSyncToken(t *testing.T, syncToken string) {
	t.Helper()
	expected := `{"calendarId":"` + environment.calendarConfiguration.CalendarID + `","syncToken":"` + syncToken + `"}`
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.Token == expected
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *eventChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by event ID; a
// later change to the same event replaces the earlier outcome.
func (environment *eventChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[calendar.EventChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Event.ID, outcome.EventID)
		outcomes[outcome.EventID] = outcome
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
