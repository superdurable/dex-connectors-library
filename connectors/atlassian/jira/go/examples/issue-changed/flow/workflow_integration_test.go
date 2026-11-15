//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package issuechanged

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
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go/internal/jirasearchfake"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	integrationScope = projectconfig.Scope{ProjectID: "jira-issue-changed", Kind: "live"}
	opsProject       = jira.IssueChangedTriggerConfiguration{ProjectID: jirasearchfake.Operations.ID, ProjectKey: "OPS", ProjectName: "Operations"}
	hrProject        = jira.IssueChangedTriggerConfiguration{ProjectKey: "HR"}
)

// TestIssueChangesStartOneFlowEachAcrossARestartWithRealDex: existing issues are a baseline, each later
// issue version starts exactly one Flow, and changes made while the process is stopped arrive after a restart.
func TestIssueChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newIssueChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	standup := environment.provider.Create(jirasearchfake.Operations, "Standup notes")
	environment.provider.Create(jirasearchfake.Operations, "Old review")
	first := environment.startTriggerProcess(t, opsProject)
	environment.waitForCursor(t, func(state *projectconfig.PollCursorState) bool { return state != nil })

	waitForNextMillisecond()
	kickoff := environment.provider.Create(jirasearchfake.Operations, "Kickoff")
	environment.provider.Update(standup, "Standup notes, edited")
	environment.provider.Create(jirasearchfake.HumanResources, "Other project")
	first.runner.WakePoller(IssueChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 2)
	require.Equal(t, jira.IssueChangeCreated, outcomes[kickoff].ChangeKind)
	require.Equal(t, "Kickoff", outcomes[kickoff].Current.Summary)
	require.Equal(t, "Standup notes, edited", outcomes[standup].Current.Summary, "the Flow reads the issue back from Jira")
	first.stop(t)

	waitForNextMillisecond()
	offline := environment.provider.Create(jirasearchfake.Operations, "Filed while stopped")
	environment.provider.Update(kickoff, "Kickoff, rescheduled")
	second := environment.startTriggerProcess(t, opsProject)
	second.runner.WakePoller(IssueChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 4)
	require.Equal(t, "Filed while stopped", outcomes[offline].Current.Summary)
	require.Equal(t, "Kickoff, rescheduled", outcomes[kickoff].Current.Summary, "the second version starts its own Flow")
	searchesBefore := len(environment.provider.Requests())
	second.runner.WakePoller(IssueChangedTriggerBinding)
	environment.waitForSearchCount(t, searchesBefore+1)
	require.Equal(t, 4, environment.deliveryCount(), "no version is delivered twice and the baseline is never delivered")
}

// TestRateLimitPausesThenDeliversWithRealDex: a 429 with Retry-After moves no cursor and loses no change.
func TestRateLimitPausesThenDeliversWithRealDex(t *testing.T) {
	environment := newIssueChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	process := environment.startTriggerProcess(t, opsProject)
	environment.waitForCursor(t, func(state *projectconfig.PollCursorState) bool { return state != nil })
	environment.provider.RateLimitNextRequests(1, "1")
	created := environment.provider.Create(jirasearchfake.Operations, "Filed during a rate limit")
	process.runner.WakePoller(IssueChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "Filed during a rate limit", outcomes[created].Current.Summary)
	statuses := []int{}
	for _, request := range environment.provider.Requests() {
		statuses = append(statuses, request.Status)
	}
	require.Contains(t, statuses, http.StatusTooManyRequests)
}

// TestChangedProjectRecordsANewBaselineWithRealDex: a cursor for another project resets without
// delivering the new project's existing issues.
func TestChangedProjectRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newIssueChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	first := environment.startTriggerProcess(t, opsProject)
	environment.waitForCursor(t, func(state *projectconfig.PollCursorState) bool { return state != nil })
	first.stop(t)

	gap := environment.provider.Create(jirasearchfake.HumanResources, "Existing in the new project")
	second := environment.startTriggerProcess(t, hrProject)
	environment.waitForCursor(t, func(state *projectconfig.PollCursorState) bool { return state != nil && state.ResetCount == 1 })
	waitForNextMillisecond()
	after := environment.provider.Create(jirasearchfake.HumanResources, "After the reset")
	second.runner.WakePoller(IssueChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", outcomes[after].Current.Summary)
	require.NotContains(t, outcomes, gap, "the new project's existing issues are its baseline")
}

// waitForNextMillisecond keeps a new issue version's updated time distinct from the baseline's.
func waitForNextMillisecond() { time.Sleep(2 * time.Millisecond) }

type issueChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *jirasearchfake.Site
	connection jira.Connection
	objects    *testsupport.ObjectStore
	mutex      sync.Mutex
	delivered  map[string]sdkgo.TriggerEvent[jira.IssueChangedEvent]
	order      []string
}

func newIssueChangedEnvironment(t *testing.T) *issueChangedEnvironment {
	t.Helper()
	fake := jirasearchfake.New(t)
	reference := sdkgo.ConnectionRef{Provider: "atlassian", Name: ConnectionName}
	client, err := jira.New(jira.Config{CloudID: jirasearchfake.CloudID, Endpoint: fake.URL, PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[jira.Credentials]{
		reference: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString("jira-token"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		},
	})
	require.NoError(t, err)
	connection, err := jira.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &issueChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[jira.IssueChangedEvent]{},
	}
}

func (environment *issueChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: jira.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: jira.IssueChangedTriggerDefinition.Trigger.TriggerName, BindingName: IssueChangedTriggerBinding,
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
// jira.NewProjectIssueTriggerRunner does, over storage that survives the process.
func (environment *issueChangedEnvironment) startTriggerProcess(t *testing.T, configuration jira.IssueChangedTriggerConfiguration) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryIssueChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[jira.IssueChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[jira.IssueChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[jira.IssueChangedEvent](counting))
	require.NoError(t, err)
	runner, err := jira.NewIssueTriggerRunner(jira.IssueTriggerRunnerConfig{
		Connection: environment.connection,
		IssueChangedRoutes: []jira.IssueChangedTriggerRoute{{
			BindingName: IssueChangedTriggerBinding, Configuration: configuration, Target: target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *issueChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *issueChangedEnvironment) waitForCursor(t *testing.T, isReady func(*projectconfig.PollCursorState) bool) {
	t.Helper()
	require.Eventually(t, func() bool {
		record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
		return err == nil && isReady(record.State)
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *issueChangedEnvironment) waitForSearchCount(t *testing.T, count int) {
	t.Helper()
	require.Eventually(t, func() bool { return len(environment.provider.Requests()) >= count }, 30*time.Second, 50*time.Millisecond)
}

func (environment *issueChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered versions, then for each version's Flow, keyed by issue key;
// a later version of the same issue replaces the earlier outcome.
func (environment *issueChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 30*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[jira.IssueChangedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.Issue.Key, outcome.IssueKey)
		outcomes[outcome.IssueKey] = outcome
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
