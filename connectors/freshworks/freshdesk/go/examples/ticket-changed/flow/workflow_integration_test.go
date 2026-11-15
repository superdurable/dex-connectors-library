//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/internal/freshdeskticketfake"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationAPIKey = "freshdeskIntegrationKey0123456789"

var integrationScope = projectconfig.Scope{ProjectID: "freshdesk-ticket-changed", Kind: "live"}

// TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex: existing tickets are a baseline, each later
// change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	existing, created, offline := environment.ticketID(1), environment.ticketID(2), environment.ticketID(3)
	environment.provider.CreateAt(existing, "Existing", time.Now().Add(-time.Hour), time.Now().Add(-10*time.Minute))
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	environment.provider.Create(created, "Double charge on order 88213")
	environment.provider.Update(existing, "Existing, escalated", 3)
	first.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, freshdesk.TicketChangeCreated, outcomes[created].ChangeKind)
	require.Equal(t, "Customer message for Double charge on order 88213", outcomes[created].Current.Ticket.Description, "the Flow reads the ticket back")
	require.Equal(t, "jane@acme.example.com", outcomes[created].Current.Requester.Email)
	require.Equal(t, freshdesk.TicketChangeUpdated, outcomes[existing].ChangeKind)
	require.Equal(t, freshdesk.TicketStatusPending, outcomes[existing].Current.Ticket.Status)
	first.stop(t)

	environment.provider.Create(offline, "Opened while stopped")
	environment.provider.Update(created, "Double charge, refunded", 4)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Opened while stopped", outcomes[offline].Current.Ticket.Subject)
	require.Equal(t, freshdesk.TicketChangeCreated, outcomes[offline].ChangeKind)
	require.Equal(t, freshdesk.TicketChangeUpdated, outcomes[created].ChangeKind)
	require.Equal(t, freshdesk.TicketStatusResolved, outcomes[created].Current.Ticket.Status)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	environment.waitForListRequestsAfter(t, environment.provider.ListRequestCount())
	require.Equal(t, 4, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
}

// TestRateLimitAndRejectedCursorRecoverWithRealDex: a 429 waits for Retry-After and then delivers, and a 400
// for the cursor's query records a new baseline without delivering the gap.
func TestRateLimitAndRejectedCursorRecoverWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	throttled, gap, afterReset := environment.ticketID(1), environment.ticketID(2), environment.ticketID(3)
	process := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	environment.provider.FailNextList(http.StatusTooManyRequests, "1", `{"code":"too_many_requests"}`)
	environment.provider.Create(throttled, "Created during a rate limit")
	process.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, freshdesk.TicketChangeCreated, outcomes[throttled].ChangeKind)
	process.stop(t)

	environment.provider.Create(gap, "Lost in the gap")
	environment.provider.FailNextList(http.StatusBadRequest, "", `{"description":"Validation failed","errors":[{"field":"updated_since","code":"invalid_value","message":"x"}]}`)
	restarted := environment.startTriggerProcess(t)
	restarted.runner.WakePoller(TicketChangedTriggerBinding)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.provider.Create(afterReset, "After the reset")
	restarted.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, "After the reset", outcomes[afterReset].Current.Ticket.Subject)
	require.NotContains(t, outcomes, gap, "changes between the rejected cursor and the new baseline are not delivered")
}

type ticketChangedEnvironment struct {
	harness      *dexHarness
	flow         *Flow
	provider     *freshdeskticketfake.Tickets
	connection   freshdesk.Connection
	objects      *testsupport.ObjectStore
	ticketIDBase int64
	mutex        sync.Mutex
	delivered    map[string]sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]
	order        []string
}

func newTicketChangedEnvironment(t *testing.T) *ticketChangedEnvironment {
	t.Helper()
	fake := freshdeskticketfake.New(t, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "freshdesk", Name: ConnectionName}
	client, err := freshdesk.New(freshdesk.Config{Domain: "acme", PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[freshdesk.Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	}, freshdesk.WithAPIBaseURL(fake.BaseURL))
	require.NoError(t, err)
	connection, err := freshdesk.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &ticketChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]{},
		// Unique ticket IDs keep Flow IDs unique across runs against one Dex server.
		ticketIDBase: (time.Now().UnixNano() / int64(time.Microsecond)) % 1_000_000_000_000 * 10,
	}
}

func (environment *ticketChangedEnvironment) ticketID(offset int64) int64 {
	return environment.ticketIDBase + offset
}

func (environment *ticketChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: freshdesk.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: freshdesk.TicketChangedTriggerDefinition.Trigger.TriggerName, BindingName: TicketChangedTriggerBinding,
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
// freshdesk.NewProjectTicketTriggerRunner does, over storage that survives the process.
func (environment *ticketChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryTicketChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[freshdesk.TicketChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[freshdesk.TicketChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[freshdesk.TicketChangedEvent](counting))
	require.NoError(t, err)
	runner, err := freshdesk.NewTicketTriggerRunner(freshdesk.TicketTriggerRunnerConfig{
		Connection: environment.connection,
		TicketChangedRoutes: []freshdesk.TicketChangedTriggerRoute{{
			BindingName: TicketChangedTriggerBinding, Target: target, Cursor: environment.cursorStore(t),
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

// waitForCommittedCursor waits until the baseline cursor is committed.
func (environment *ticketChangedEnvironment) waitForCommittedCursor(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
}

// waitForListRequestsAfter waits for one more completed poll and an empty inbox.
func (environment *ticketChangedEnvironment) waitForListRequestsAfter(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.provider.ListRequestCount() > count }, 30*time.Second, 50*time.Millisecond)
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, integrationScope, environment.inboxKey())
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *ticketChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by ticket ID; a
// later change to the same ticket replaces the earlier outcome.
func (environment *ticketChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[int64]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 45*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[freshdesk.TicketChangedEvent], 0, len(environment.order))
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
