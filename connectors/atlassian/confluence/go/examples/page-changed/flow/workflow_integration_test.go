//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pagechanged

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go/internal/confluencepagefake"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/confluence/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "confluence-page-changed", Kind: "live"}

// TestPageChangesStartOneFlowEachAcrossARestartWithRealDex: recently changed pages are a baseline, each
// later page version starts exactly one Flow, and versions saved while the process is stopped arrive after
// a restart whose first search answers 429.
func TestPageChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newPageChangedEnvironment(t, "OPS")
	ctx := integrationContext(t, 3*time.Minute)
	handbook := environment.site.Publish("OPS", "Handbook", "Welcome")
	environment.site.Publish("HR", "Payroll calendar", "Not in the watched space")
	first := environment.startTriggerProcess(t)
	environment.waitForCommit(t, 1)

	policy := environment.site.Publish("OPS", "Remote work policy", "Two days a week")
	environment.site.Edit(handbook, "Handbook", "Welcome, and read the policy")
	environment.site.Publish("HR", "Benefits", "Still not watched")
	first.runner.WakePoller(PageChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, confluence.PageChangeCreated, outcomes[policy].ChangeKind)
	require.Equal(t, "Remote work policy", outcomes[policy].Title)
	require.Contains(t, outcomes[policy].Body, "Two days a week", "the Flow reads the page back as Markdown")
	require.Equal(t, confluence.PageChangeUpdated, outcomes[handbook].ChangeKind)
	require.Equal(t, 2, outcomes[handbook].DeliveredVersion)
	first.stop(t)

	environment.site.Edit(policy, "Remote work policy", "Three days a week")
	offline := environment.site.Publish("OPS", "Offsite agenda", "Planned while stopped")
	environment.site.ThrottleSearches(1, 1)
	searchesBeforeRestart := environment.site.SearchRequests()
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(PageChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Contains(t, outcomes[offline].Body, "Planned while stopped")
	require.Equal(t, 2, outcomes[policy].DeliveredVersion)
	require.Contains(t, outcomes[policy].Body, "Three days a week")
	require.Greater(t, environment.site.SearchRequests(), searchesBeforeRestart+1, "the throttled search was retried")
	second.runner.WakePoller(PageChangedTriggerBinding)
	environment.waitForCommit(t, environment.cursorState(t).CommitCount+1)
	require.Equal(t, 4, environment.deliveryCount(), "no version is delivered twice and the baseline is never delivered")
}

// TestChangedSpaceRecordsANewBaselineWithRealDex: a binding moved to another space reseeds without
// delivering that space's existing pages.
func TestChangedSpaceRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newPageChangedEnvironment(t, "OPS")
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t)
	environment.waitForCommit(t, 1)
	first.stop(t)

	environment.site.Publish("HR", "Existing HR page", "Already there")
	environment.configuration = confluence.PageChangedTriggerConfiguration{SpaceKey: "HR", SpaceName: "Human Resources"}
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	afterReset := environment.site.Publish("HR", "Leave policy", "After the reset")
	second.runner.WakePoller(PageChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "Leave policy", outcomes[afterReset].Title)
	require.Len(t, outcomes, 1, "existing pages in the new space are a baseline")
}

type pageChangedEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	site          *confluencepagefake.Site
	connection    confluence.Connection
	objects       *testsupport.ObjectStore
	configuration confluence.PageChangedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[confluence.PageChangedEvent]
	order         []string
}

// newPageChangedEnvironment uses a unique site ID, so Flow IDs are unique per run against a shared server.
func newPageChangedEnvironment(t *testing.T, spaceKey string) *pageChangedEnvironment {
	t.Helper()
	now := time.Now().UnixNano()
	cloudID := fmt.Sprintf("%08x-0000-4000-8000-%012x", uint32(now>>32), uint64(now)&0xffffffffffff)
	site := confluencepagefake.New(t, cloudID)
	reference := sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName}
	client, err := confluence.New(confluence.Config{Endpoint: site.URL, CloudID: cloudID, PollInterval: 10 * time.Second},
		sdkgo.StaticCredentialProvider[confluence.Credentials]{reference: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString(confluencepagefake.AccessToken), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		}})
	require.NoError(t, err)
	connection, err := confluence.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &pageChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, site: site, connection: connection,
		objects: testsupport.NewObjectStore(), configuration: confluence.PageChangedTriggerConfiguration{SpaceKey: spaceKey},
		delivered: map[string]sdkgo.TriggerEvent[confluence.PageChangedEvent]{},
	}
}

func (environment *pageChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: confluence.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: confluence.PageChangedTriggerDefinition.Trigger.TriggerName, BindingName: PageChangedTriggerBinding,
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
// confluence.NewProjectPageTriggerRunner does, over storage that survives the process.
func (environment *pageChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryPageChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[confluence.PageChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[confluence.PageChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[confluence.PageChangedEvent](counting))
	require.NoError(t, err)
	runner, err := confluence.NewPageTriggerRunner(confluence.PageTriggerRunnerConfig{
		Connection: environment.connection,
		PageChangedRoutes: []confluence.PageChangedTriggerRoute{{
			BindingName: PageChangedTriggerBinding, Configuration: environment.configuration,
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

func (environment *pageChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *pageChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCommit waits until the cursor has committed at least commitCount times with no scan in progress.
func (environment *pageChangedEnvironment) waitForCommit(t *testing.T, commitCount int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.CommitCount >= commitCount && !containsScan(state.Token)
	}, 60*time.Second, 50*time.Millisecond)
}

func containsScan(token string) bool { return strings.Contains(token, `"scan":`) }

func (environment *pageChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered versions, then for each version's Flow, keyed by page ID; a
// later version of the same page replaces the earlier outcome.
func (environment *pageChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 60*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[confluence.PageChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Page.ID, outcome.PageID)
		outcomes[outcome.PageID] = outcome
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
