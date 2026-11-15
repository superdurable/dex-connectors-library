//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package invoicechanged

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/xero/go/internal/xeroinvoicefake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "xero-invoice-changed", Kind: "live"}

type invoiceChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *xeroinvoicefake.Organisation
	connection xero.Connection
	objects    *testsupport.ObjectStore
	// configuration is the saved invoice filter; a test changes it between processes.
	configuration xero.InvoiceChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[xero.InvoiceChangedEvent]
	order         []string
}

func newInvoiceChangedEnvironment(t *testing.T, configuration xero.InvoiceChangedTriggerConfiguration) *invoiceChangedEnvironment {
	t.Helper()
	fake := xeroinvoicefake.New(t)
	reference := sdkgo.ConnectionRef{Provider: "xero", Name: ConnectionName}
	// The 30-second minimum interval also bounds the pause after a rate limit.
	client, err := xero.New(xero.Config{PollInterval: 30 * time.Second}, sdkgo.StaticCredentialProvider[xero.Credentials]{reference: {
		AuthMethodID: xero.CustomConnectionAuthMethodID, ClientID: "0F1E2D3C4B5A69788796A5B4C3D2E1F0",
		ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString("xero-invoice-watcher-token"),
	}}, xero.WithLocalProviderURL(fake.URL))
	require.NoError(t, err)
	connection, err := xero.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &invoiceChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection, configuration: configuration,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[xero.InvoiceChangedEvent]{},
	}
}

func (environment *invoiceChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: xero.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: xero.InvoiceChangedTriggerDefinition.Trigger.TriggerName, BindingName: InvoiceChangedTriggerBinding,
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
// xero.NewProjectInvoiceTriggerRunner does, over storage that survives the process.
func (environment *invoiceChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryInvoiceChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[xero.InvoiceChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[xero.InvoiceChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[xero.InvoiceChangedEvent](counting))
	require.NoError(t, err)
	runner, err := xero.NewInvoiceTriggerRunner(xero.InvoiceTriggerRunnerConfig{
		Connection: environment.connection,
		InvoiceChangedRoutes: []xero.InvoiceChangedTriggerRoute{{
			BindingName: InvoiceChangedTriggerBinding, Configuration: environment.configuration,
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

func (environment *invoiceChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *invoiceChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

func (environment *invoiceChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// lastDeliveredUpdate is the newest UpdatedDateUTC delivered so far.
func (environment *invoiceChangedEnvironment) lastDeliveredUpdate() time.Time {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	var newest time.Time
	for _, event := range environment.delivered {
		if event.Payload.Invoice.UpdatedAt.After(newest) {
			newest = event.Payload.Invoice.UpdatedAt
		}
	}
	return newest
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by invoice ID; a
// later change to the same invoice replaces the earlier outcome.
func (environment *invoiceChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 75*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[xero.InvoiceChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Invoice.InvoiceID, outcome.InvoiceID)
		outcomes[outcome.InvoiceID] = outcome
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
