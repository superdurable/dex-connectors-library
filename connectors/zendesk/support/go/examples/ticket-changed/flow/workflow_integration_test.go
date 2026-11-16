//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/internal/zendeskexportfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "zendesk-ticket-changed", Kind: "live"}

// TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex: existing tickets are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart that
// first answers 429.
func TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t, support.TicketChangedTriggerConfiguration{IncludeDeleted: true})
	ctx := integrationContext(t, 2*time.Minute)
	existing, created, spam, offline := environment.ticketID(1), environment.ticketID(2), environment.ticketID(3), environment.ticketID(4)
	environment.provider.Create(existing, "Existing ticket")
	environment.provider.Create(spam, "Spam")
	first := environment.startTriggerProcess(t)
	environment.waitForCaughtUpCursor(t)

	environment.provider.Create(created, "Double charge on order 88213")
	environment.provider.Update(existing, "pending")
	environment.provider.Delete(spam)
	first.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, support.TicketChangeCreated, outcomes[created].ChangeKind)
	require.Equal(t, "Opened: Double charge on order 88213", outcomes[created].Current.LatestComments[0].Body)
	require.Equal(t, support.TicketChangeUpdated, outcomes[existing].ChangeKind)
	require.Equal(t, support.TicketStatusPending, outcomes[existing].Current.Ticket.Status, "the Flow reads the ticket back from Zendesk")
	require.Equal(t, support.TicketChangeDeleted, outcomes[spam].ChangeKind)
	require.Nil(t, outcomes[spam].Current, "a deleted ticket is not read back")
	first.stop(t)

	environment.provider.Create(offline, "Opened while stopped")
	environment.provider.Update(created, "solved")
	environment.provider.RateLimitNextRequests(1, "1")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 5)
	require.Equal(t, "Opened while stopped", outcomes[offline].Current.Ticket.Subject)
	require.Equal(t, support.TicketStatusSolved, outcomes[created].Current.Ticket.Status)
	environment.waitForCaughtUpCursor(t)
	require.Equal(t, 5, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
	require.Contains(t, environment.requestStatuses(), 429)
}

// TestRejectedCursorRestartsWithoutAGapWithRealDex: a cursor Zendesk answers with 422 restarts the export
// from the cursor's last change time, so a change made in between still starts its Flow. The restart may
// repeat up to the last minute, so the baseline ticket's delivery is not asserted either way.
func TestRejectedCursorRestartsWithoutAGapWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t, support.TicketChangedTriggerConfiguration{})
	ctx := integrationContext(t, 2*time.Minute)
	before, gap := environment.ticketID(1), environment.ticketID(2)
	environment.provider.Create(before, "Before the restart")
	first := environment.startTriggerProcess(t)
	environment.waitForCaughtUpCursor(t)
	first.stop(t)

	environment.provider.InvalidateCursors()
	environment.provider.Create(gap, "Created after the cursor expired")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	require.Eventually(t, func() bool { return environment.isDelivered(gap) }, 30*time.Second, 50*time.Millisecond)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "Created after the cursor expired", outcomes[gap].Current.Ticket.Subject)
	environment.waitForCaughtUpCursor(t)
	require.Zero(t, environment.cursorState(t).ResetCount, "a rejected cursor restarts by time instead of reseeding")
	require.Contains(t, environment.requestStatuses(), 422)
}

type ticketChangedEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	provider      *zendeskexportfake.Account
	connection    support.Connection
	objects       *testsupport.ObjectStore
	configuration support.TicketChangedTriggerConfiguration
	// ticketBase keeps ticket IDs, and therefore Flow IDs, unique per run.
	ticketBase int64
	mutex      sync.Mutex
	delivered  map[string]sdkgo.TriggerEvent[support.TicketChangedEvent]
	order      []string
}

func newTicketChangedEnvironment(t *testing.T, configuration support.TicketChangedTriggerConfiguration) *ticketChangedEnvironment {
	t.Helper()
	fake := zendeskexportfake.New(t)
	reference := sdkgo.ConnectionRef{Provider: "zendesk", Name: ConnectionName}
	client, err := support.New(support.Config{Subdomain: "acme", PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[support.Credentials]{
		reference: {Email: zendeskexportfake.Email, APIToken: sdkgo.NewSecretString(zendeskexportfake.APIToken)},
	}, support.WithAPIBaseURL(fake.APIBaseURL()), support.WithIncrementalExportRequestsPerMinute(600))
	require.NoError(t, err)
	connection, err := support.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &ticketChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, configuration: configuration,
		objects: testsupport.NewObjectStore(), ticketBase: time.Now().UnixMicro() % 1_000_000_000 * 10,
		delivered: map[string]sdkgo.TriggerEvent[support.TicketChangedEvent]{},
	}
}

func (environment *ticketChangedEnvironment) ticketID(offset int64) int64 {
	return environment.ticketBase + offset
}

func (environment *ticketChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: support.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: support.TicketChangedTriggerDefinition.Trigger.TriggerName, BindingName: TicketChangedTriggerBinding,
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
// support.NewProjectTicketTriggerRunner does, over storage that survives the process.
func (environment *ticketChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryTicketChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[support.TicketChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[support.TicketChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[support.TicketChangedEvent](counting))
	require.NoError(t, err)
	runner, err := support.NewTicketTriggerRunner(support.TicketTriggerRunnerConfig{
		Connection: environment.connection,
		TicketChangedRoutes: []support.TicketChangedTriggerRoute{{
			BindingName: TicketChangedTriggerBinding, Configuration: environment.configuration,
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

func (environment *ticketChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *ticketChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCaughtUpCursor waits until the committed cursor ends at the fake's latest change.
func (environment *ticketChangedEnvironment) waitForCaughtUpCursor(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && strings.Contains(state.Token, `"afterCursor":"`+environment.provider.Cursor()+`"`)
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *ticketChangedEnvironment) isDelivered(ticketID int64) bool {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	for _, event := range environment.delivered {
		if event.Payload.Ticket.ID == ticketID {
			return true
		}
	}
	return false
}

func (environment *ticketChangedEnvironment) requestStatuses() []int {
	var statuses []int
	for _, request := range environment.provider.Requests() {
		statuses = append(statuses, request.Status)
	}
	return statuses
}

func (environment *ticketChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by event ID; a
// later change to the same event replaces the earlier outcome.
func (environment *ticketChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[int64]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[support.TicketChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Ticket.ID, outcome.TicketID)
		outcomes[outcome.TicketID] = outcome
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
