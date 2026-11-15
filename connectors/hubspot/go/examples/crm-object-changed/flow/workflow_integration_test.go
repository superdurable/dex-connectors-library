//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crmobjectchanged

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
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/internal/hubspotsearchfake"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "hubspot-crm-object-changed", Kind: "live"}

// TestCRMObjectChangesStartOneFlowEachAcrossARestartWithRealDex: existing records are a baseline, each
// later change starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestCRMObjectChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newCRMObjectChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	renewal := environment.provider.Create(map[string]string{"dealname": "Renewal", "dealstage": "appointmentscheduled"})
	environment.provider.Create(map[string]string{"dealname": "Untouched"})
	first := environment.startTriggerProcess(t)
	environment.waitForCursor(t)

	expansion := environment.provider.Create(map[string]string{"dealname": "Expansion", "dealstage": "appointmentscheduled"})
	environment.provider.Update(renewal, map[string]string{"dealstage": "qualifiedtobuy"})
	first.runner.WakePoller(CRMObjectChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, hubspot.CRMObjectChangeCreated, outcomes[expansion].ChangeKind)
	require.Equal(t, "Expansion", outcomes[expansion].Current.Properties["dealname"])
	require.Equal(t, hubspot.CRMObjectChangeUpdated, outcomes[renewal].ChangeKind)
	require.Equal(t, "qualifiedtobuy", outcomes[renewal].Current.Properties["dealstage"], "the Flow reads the record back from HubSpot")
	first.stop(t)

	offline := environment.provider.Create(map[string]string{"dealname": "Created while stopped"})
	environment.provider.Update(expansion, map[string]string{"dealstage": "closedwon"})
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(CRMObjectChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Created while stopped", outcomes[offline].Current.Properties["dealname"])
	require.Equal(t, hubspot.CRMObjectChangeUpdated, outcomes[expansion].ChangeKind)
	require.Equal(t, "closedwon", outcomes[expansion].Current.Properties["dealstage"])
	second.runner.WakePoller(CRMObjectChangedTriggerBinding)
	environment.waitForSearchRequests(t, environment.provider.SearchRequestCount()+1)
	require.Equal(t, 4, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
}

// TestRateLimitedSearchPausesThenDeliversWithRealDex: a 429 with Retry-After pauses polling, and the change
// arrives once the pause ends.
func TestRateLimitedSearchPausesThenDeliversWithRealDex(t *testing.T) {
	environment := newCRMObjectChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Create(map[string]string{"dealname": "Baseline"})
	process := environment.startTriggerProcess(t)
	environment.waitForCursor(t)

	environment.provider.FailNextSearches(hubspotsearchfake.FailedResponse{
		Status: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"1"}}, Body: `{"category":"RATE_LIMITS","policyName":"SECONDLY"}`,
	})
	limited := environment.provider.SearchRequestCount()
	created := environment.provider.Create(map[string]string{"dealname": "After the pause"})
	process.runner.WakePoller(CRMObjectChangedTriggerBinding)
	environment.waitForSearchRequests(t, limited+1)
	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the pause", outcomes[created].Current.Properties["dealname"])
	require.Greater(t, environment.provider.SearchRequestCount(), limited+1, "the poll after the pause searched again")
}

type crmObjectChangedEnvironment struct {
	harness              *dexHarness
	flow                 *Flow
	provider             *hubspotsearchfake.Server
	connection           hubspot.Connection
	objects              *testsupport.ObjectStore
	triggerConfiguration hubspot.CRMObjectChangedTriggerConfiguration
	mutex                sync.Mutex
	delivered            map[string]sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]
	order                []string
}

func newCRMObjectChangedEnvironment(t *testing.T) *crmObjectChangedEnvironment {
	t.Helper()
	// A start time unique to the run keeps Trigger event IDs, and so Flow IDs, unique on a shared Dex.
	fake := hubspotsearchfake.New(t, "deals", time.Now())
	reference := sdkgo.ConnectionRef{Provider: "hubspot", Name: ConnectionName}
	client, err := hubspot.New(hubspot.Config{Endpoint: fake.URL, PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{
		reference: {AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(hubspotsearchfake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := hubspot.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &crmObjectChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects:              testsupport.NewObjectStore(),
		triggerConfiguration: hubspot.CRMObjectChangedTriggerConfiguration{ObjectType: hubspot.ObjectTypeDeals, Properties: DeliveredProperties},
		delivered:            map[string]sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]{},
	}
}

func (environment *crmObjectChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: hubspot.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: hubspot.CRMObjectChangedTriggerDefinition.Trigger.TriggerName, BindingName: CRMObjectChangedTriggerBinding,
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
// hubspot.NewProjectCRMObjectTriggerRunner does, over storage that survives the process.
func (environment *crmObjectChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryCRMObjectChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[hubspot.CRMObjectChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[hubspot.CRMObjectChangedEvent](counting))
	require.NoError(t, err)
	runner, err := hubspot.NewCRMObjectTriggerRunner(hubspot.CRMObjectTriggerRunnerConfig{
		Connection: environment.connection,
		CRMObjectChangedRoutes: []hubspot.CRMObjectChangedTriggerRoute{{
			BindingName: CRMObjectChangedTriggerBinding, Configuration: environment.triggerConfiguration,
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

func (environment *crmObjectChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *crmObjectChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCursor waits until the binding committed its first cursor.
func (environment *crmObjectChangedEnvironment) waitForCursor(t *testing.T) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
}

func (environment *crmObjectChangedEnvironment) waitForSearchRequests(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.provider.SearchRequestCount() >= count }, 30*time.Second, 50*time.Millisecond)
}

func (environment *crmObjectChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by record ID; a
// later change to the same record replaces the earlier outcome.
func (environment *crmObjectChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[hubspot.CRMObjectChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Object.ID, outcome.ObjectID)
		outcomes[outcome.ObjectID] = outcome
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
