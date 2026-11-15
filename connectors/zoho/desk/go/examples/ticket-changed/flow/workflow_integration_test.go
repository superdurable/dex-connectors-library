//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ticketchanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go/internal/desksearchfake"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationDepartmentID = "1892000000006907"

var integrationScope = projectconfig.Scope{ProjectID: "zoho-desk-ticket-changed", Kind: "live"}

// TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex: recent tickets are a baseline, each later
// ticket version starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestTicketChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	existing := environment.provider.Create("Existing before the baseline", integrationDepartmentID)
	environment.provider.Create("Other department", "1892000000082069")
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	created := environment.provider.Create("Refund request", integrationDepartmentID)
	environment.provider.Update(existing, "On Hold")
	first.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, desk.TicketChangeCreated, outcomes[created].ChangeKind)
	require.Equal(t, "Refund request", outcomes[created].Current.Ticket.Subject)
	require.Equal(t, "<div>Refund request</div>", outcomes[created].Current.Ticket.DescriptionHTML, "the Flow reads the full ticket back")
	require.Equal(t, desk.TicketChangeUpdated, outcomes[existing].ChangeKind)
	require.Equal(t, desk.TicketStatusOnHold, outcomes[existing].Current.Ticket.Status)
	first.stop(t)

	offline := environment.provider.Create("Opened while stopped", integrationDepartmentID)
	environment.provider.Update(created, "Closed")
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Opened while stopped", outcomes[offline].Current.Ticket.Subject)
	require.Equal(t, desk.TicketStatusClosed, outcomes[created].Current.Ticket.Status)
	second.runner.WakePoller(TicketChangedTriggerBinding)
	environment.waitForSearchesAfter(t, len(environment.provider.Searches()))
	require.Equal(t, 4, environment.deliveryCount(), "no ticket version is delivered twice and the baseline is never delivered")
	for _, search := range environment.provider.Searches() {
		require.Equal(t, integrationDepartmentID, search.DepartmentID)
	}
}

// TestRejectedTimeRangeRecordsANewBaselineWithRealDex: a 422 naming modifiedTimeRange reseeds the cursor.
func TestRejectedTimeRangeRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newTicketChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	process := environment.startTriggerProcess(t)
	environment.waitForCommittedCursor(t)

	environment.provider.RejectNextRanges(1)
	process.runner.WakePoller(TicketChangedTriggerBinding)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)

	afterReset := environment.provider.Create("After the reset", integrationDepartmentID)
	process.runner.WakePoller(TicketChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes[afterReset].Current.Ticket.Subject)
}

type ticketChangedEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	provider      *desksearchfake.Desk
	connection    desk.Connection
	objects       *testsupport.ObjectStore
	configuration desk.TicketChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[desk.TicketChangedEvent]
	order         []string
}

func newTicketChangedEnvironment(t *testing.T) *ticketChangedEnvironment {
	t.Helper()
	fake := desksearchfake.New(t, time.Time{})
	reference := sdkgo.ConnectionRef{Provider: "zoho", Name: ConnectionName}
	client, err := desk.New(desk.Config{OrgID: desksearchfake.OrganizationID, PollInterval: 15 * time.Second}, sdkgo.StaticCredentialProvider[desk.Credentials]{
		reference: {AuthMethodID: desk.USDataCenterAuthMethodID, AccessToken: sdkgo.NewSecretString(desksearchfake.AccessToken)},
	}, desk.WithAPIBaseURL(fake.URL+"/api/v1"))
	require.NoError(t, err)
	connection, err := desk.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &ticketChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[desk.TicketChangedEvent]{},
		configuration: desk.TicketChangedTriggerConfiguration{DepartmentID: integrationDepartmentID, DepartmentName: "Billing"},
	}
}

func (environment *ticketChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: desk.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: desk.TicketChangedTriggerDefinition.Trigger.TriggerName, BindingName: TicketChangedTriggerBinding,
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
// desk.NewProjectTicketTriggerRunner does, over storage that survives the process.
func (environment *ticketChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryTicketChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[desk.TicketChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[desk.TicketChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[desk.TicketChangedEvent](counting))
	require.NoError(t, err)
	runner, err := desk.NewTicketTriggerRunner(desk.TicketTriggerRunnerConfig{
		Connection: environment.connection,
		TicketChangedRoutes: []desk.TicketChangedTriggerRoute{{
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

// waitForCommittedCursor waits until the baseline is committed.
func (environment *ticketChangedEnvironment) waitForCommittedCursor(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
}

// waitForSearchesAfter waits until the fake received more than count searches.
func (environment *ticketChangedEnvironment) waitForSearchesAfter(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(environment.provider.Searches()) > count }, 30*time.Second, 50*time.Millisecond)
}

func (environment *ticketChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by ticket ID; a
// later version of the same ticket replaces the earlier outcome.
func (environment *ticketChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[desk.TicketChangedEvent], 0, len(environment.order))
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
