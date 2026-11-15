//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meetingchanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/internal/zoommeetingsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "zoom-meeting-changed", Kind: "live"}

// TestMeetingChangesStartOneFlowEachAcrossARestartWithRealDex: existing meetings are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestMeetingChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newMeetingChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	consultation, review, kickoff, offline := environment.meetingID(0), environment.meetingID(1), environment.meetingID(2), environment.meetingID(3)
	environment.provider.Schedule(consultation, "Consultation", 1)
	environment.provider.Schedule(review, "Team review", 2)
	first := environment.startTriggerProcess(t)
	environment.waitForSnapshot(t, 2)

	environment.provider.Schedule(kickoff, "Kickoff", 3)
	environment.provider.Reschedule(consultation, 2)
	environment.provider.Delete(review)
	first.runner.WakePoller(MeetingChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, zoom.MeetingChangeScheduled, outcomes[kickoff].ChangeKind)
	require.Equal(t, "Kickoff", outcomes[kickoff].Current.Topic)
	require.True(t, outcomes[kickoff].Current.Settings.WaitingRoom, "the Flow reads the full meeting back from Zoom")
	require.Equal(t, zoom.MeetingChangeUpdated, outcomes[consultation].ChangeKind)
	require.Equal(t, "Consultation", outcomes[consultation].Current.Topic)
	require.Equal(t, zoom.MeetingChangeRemoved, outcomes[review].ChangeKind)
	require.Nil(t, outcomes[review].Current, "a removed meeting is not read back")
	first.stop(t)

	environment.provider.Schedule(offline, "Planned while stopped", 4)
	environment.provider.Rename(kickoff, "Kickoff moved")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(MeetingChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 5)
	require.Equal(t, "Planned while stopped", outcomes[offline].Current.Topic)
	require.Equal(t, zoom.MeetingChangeUpdated, outcomes[kickoff].ChangeKind)
	require.Equal(t, "Kickoff moved", outcomes[kickoff].Current.Topic)
	environment.waitForSnapshot(t, 3)
	require.Equal(t, 5, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
}

// TestAnotherZoomUsersListingRecordsANewBaselineWithRealDex: a listing that belongs to another Zoom user
// than the stored snapshot reseeds instead of reporting every meeting as removed and scheduled.
func TestAnotherZoomUsersListingRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newMeetingChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Schedule(environment.meetingID(0), "Consultation", 1)
	first := environment.startTriggerProcess(t)
	environment.waitForSnapshot(t, 1)
	first.stop(t)

	environment.provider.SetHostID("zoom-host-2")
	environment.provider.Schedule(environment.meetingID(1), "Lost in the gap", 2)
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForSnapshot(t, 2)
	afterReset := environment.meetingID(2)
	environment.provider.Schedule(afterReset, "After the reset", 3)
	second.runner.WakePoller(MeetingChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes[afterReset].Current.Topic)
	require.Len(t, outcomes, 1, "meetings in the gap are part of the new baseline")
	require.JSONEq(t, `{"version":1,"hostId":"zoom-host-2","sequence":1}`, environment.cursorState(t).Token)
}

type meetingChangedEnvironment struct {
	harness        *dexHarness
	flow           *Flow
	provider       *zoommeetingsfake.Meetings
	connection     zoom.Connection
	objects        *testsupport.ObjectStore
	firstMeetingID int64
	mutex          sync.Mutex
	delivered      map[string]sdkgo.TriggerEvent[zoom.MeetingChangedEvent]
	order          []string
}

func newMeetingChangedEnvironment(t *testing.T) *meetingChangedEnvironment {
	t.Helper()
	fake := zoommeetingsfake.New(t, "zoom-host-1")
	reference := sdkgo.ConnectionRef{Provider: "zoom", Name: ConnectionName}
	client, err := zoom.New(zoom.Config{Endpoint: fake.URL, PollInterval: 30 * time.Second}, sdkgo.StaticCredentialProvider[zoom.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString(zoommeetingsfake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := zoom.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &meetingChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[zoom.MeetingChangedEvent]{},
		// Unique meeting IDs keep Flow IDs unique per run against a long-lived Dex server.
		firstMeetingID: 10_000_000_000 + (time.Now().UnixNano()%1_000_000_000)*10,
	}
}

func (environment *meetingChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: zoom.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: zoom.MeetingChangedTriggerDefinition.Trigger.TriggerName, BindingName: MeetingChangedTriggerBinding,
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
// zoom.NewProjectMeetingTriggerRunner does, over storage that survives the process.
func (environment *meetingChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryMeetingChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[zoom.MeetingChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[zoom.MeetingChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[zoom.MeetingChangedEvent](counting))
	require.NoError(t, err)
	runner, err := zoom.NewMeetingTriggerRunner(zoom.MeetingTriggerRunnerConfig{
		Connection: environment.connection,
		MeetingChangedRoutes: []zoom.MeetingChangedTriggerRoute{{
			BindingName: MeetingChangedTriggerBinding,
			Target:      target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *meetingChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *meetingChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForSnapshot waits until the committed cursor holds exactly meetingCount meetings.
func (environment *meetingChangedEnvironment) waitForSnapshot(t *testing.T, meetingCount int) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.Snapshot != nil && len(state.Snapshot.Entries) == meetingCount
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *meetingChangedEnvironment) meetingID(offset int64) int64 {
	return environment.firstMeetingID + offset
}

func (environment *meetingChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by event ID; a
// later change to the same event replaces the earlier outcome.
func (environment *meetingChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[int64]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[zoom.MeetingChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[int64]ChangeOutcome{}
	for _, event := range events {
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.Meeting.ID, outcome.MeetingID)
		outcomes[outcome.MeetingID] = outcome
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
