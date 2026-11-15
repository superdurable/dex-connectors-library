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
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go/internal/gorgiaseventfake"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	integrationEmail  = "agent@example.com"
	integrationAPIKey = "gorgiasIntegrationKey0123456789"
)

var integrationScope = projectconfig.Scope{ProjectID: "gorgias-ticket-changed", Kind: "live"}

// TestTicketEventsStartOneFlowEachAcrossARestartWithRealDex: earlier events are a baseline, each later
// ticket event starts exactly one Flow, and events recorded while the process is stopped arrive after a restart.
func TestTicketEventsStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	existing, created, offline := environment.ticketID(1), environment.ticketID(2), environment.ticketID(3)
	environment.provider.SetTicket(existing, "Existing", "open")
	environment.provider.RecordTicketEvent("ticket-created", existing, 0)
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	environment.provider.SetTicket(created, "Double charge on order 88213", "open", "billing")
	createdEvent := environment.provider.RecordTicketEvent("ticket-created", created, 0)
	environment.provider.SetTicket(existing, "Existing", "closed")
	closedEvent := environment.provider.RecordTicketEvent("ticket-closed", existing, 12)
	first.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, gorgias.TicketEventCreated, outcomes[createdEvent].EventType)
	require.Equal(t, "Customer message for Double charge on order 88213", outcomes[createdEvent].Current.LatestMessages[0].Body, "the Flow reads the ticket back")
	require.Equal(t, []string{"billing"}, outcomes[createdEvent].Current.Ticket.Tags)
	require.Equal(t, "ada@example.com", outcomes[createdEvent].Current.Requester.Email)
	require.Equal(t, gorgias.TicketEventClosed, outcomes[closedEvent].EventType)
	require.Equal(t, gorgias.TicketStatusClosed, outcomes[closedEvent].Current.Ticket.Status)
	first.stop(t)

	environment.provider.SetTicket(offline, "Opened while stopped", "open")
	offlineEvent := environment.provider.RecordTicketEvent("ticket-created", offline, 0)
	reopenedEvent := environment.provider.RecordTicketEvent("ticket-reopened", existing, 12)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Opened while stopped", outcomes[offlineEvent].Current.Ticket.Subject)
	require.Equal(t, gorgias.TicketEventReopened, outcomes[reopenedEvent].EventType)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	environment.waitForEventRequestsAfter(t, environment.provider.EventRequestCount())
	require.Equal(t, 4, environment.deliveryCount(), "no event is delivered twice and the baseline is never delivered")
}

// TestRateLimitAndRejectedRequestRecoverWithRealDex: a 429 waits for Retry-After and then delivers, and a
// 400 backs off and then delivers the event recorded meanwhile from the same cursor, without a reseed.
func TestRateLimitAndRejectedRequestRecoverWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	throttled, rejected := environment.ticketID(1), environment.ticketID(2)
	process := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	environment.provider.FailNextEvents(http.StatusTooManyRequests, "1", `{"error":{"msg":"too_many_requests"}}`)
	environment.provider.SetTicket(throttled, "Created during a rate limit", "open")
	throttledEvent := environment.provider.RecordTicketEvent("ticket-created", throttled, 0)
	process.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, throttled, outcomes[throttledEvent].TicketID)
	process.stop(t)

	environment.provider.SetTicket(rejected, "Recorded while Gorgias rejected a poll", "open")
	rejectedEvent := environment.provider.RecordTicketEvent("ticket-created", rejected, 0)
	environment.provider.FailNextEvents(http.StatusBadRequest, "", `{"error":{"msg":"invalid_query","data":{"created_datetime":["bad"]}}}`)
	restarted := environment.startTriggerProcess(t)
	restarted.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, "Recorded while Gorgias rejected a poll", outcomes[rejectedEvent].Current.Ticket.Subject)
	state := environment.cursorState(t)
	require.NotNil(t, state)
	require.Zero(t, state.ResetCount, "a 400 never records a new baseline")
}

type ticketChangedEnvironment struct {
	harness      *dexHarness
	flow         *Flow
	provider     *gorgiaseventfake.Helpdesk
	connection   gorgias.Connection
	objects      *testsupport.ObjectStore
	ticketIDBase int64
	mutex        sync.Mutex
	delivered    map[string]sdkgo.TriggerEvent[gorgias.TicketChangedEvent]
	order        []string
}

func newTicketChangedEnvironment(t *testing.T) *ticketChangedEnvironment {
	t.Helper()
	fake := gorgiaseventfake.New(t, integrationEmail, integrationAPIKey)
	reference := sdkgo.ConnectionRef{Provider: "gorgias", Name: ConnectionName}
	client, err := gorgias.New(gorgias.Config{Domain: "acme", PollInterval: 10 * time.Second}, sdkgo.StaticCredentialProvider[gorgias.Credentials]{
		reference: {Email: integrationEmail, APIKey: sdkgo.NewSecretString(integrationAPIKey)},
	}, gorgias.WithAPIBaseURL(fake.BaseURL))
	require.NoError(t, err)
	connection, err := gorgias.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	// Unique ticket and event IDs keep Flow IDs unique across runs against one Dex server.
	idBase := (time.Now().UnixNano() / int64(time.Microsecond)) % 1_000_000_000_000 * 10
	fake.StartEventIDsAfter(idBase)
	return &ticketChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[gorgias.TicketChangedEvent]{},
		ticketIDBase: idBase,
	}
}

func (environment *ticketChangedEnvironment) ticketID(offset int64) int64 {
	return environment.ticketIDBase + offset
}

func (environment *ticketChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: gorgias.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: gorgias.TicketChangedTriggerDefinition.Trigger.TriggerName, BindingName: TicketChangedTriggerBinding,
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
// gorgias.NewProjectTicketTriggerRunner does, over storage that survives the process.
func (environment *ticketChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryTicketChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[gorgias.TicketChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[gorgias.TicketChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[gorgias.TicketChangedEvent](counting))
	require.NoError(t, err)
	runner, err := gorgias.NewTicketTriggerRunner(gorgias.TicketTriggerRunnerConfig{
		Connection: environment.connection,
		TicketChangedRoutes: []gorgias.TicketChangedTriggerRoute{{
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

// waitForEventRequestsAfter waits for one more completed poll and an empty inbox.
func (environment *ticketChangedEnvironment) waitForEventRequestsAfter(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.provider.EventRequestCount() > count }, 30*time.Second, 50*time.Millisecond)
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

// waitForOutcomes waits for count delivered events, then for each event's Flow, keyed by Gorgias event ID.
func (environment *ticketChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[int64]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 45*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[gorgias.TicketChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.TicketID, outcome.TicketID)
		outcomes[event.Payload.EventID] = outcome
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
