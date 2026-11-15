//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package integrationtest_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/sdk-go/dex"
)

var pollTriggerScope = projectconfig.Scope{ProjectID: "poll-trigger-tests", Kind: "live"}

var pollConnection = sdkgo.ConnectionRef{Provider: "changelog", Name: "local"}

var pollDefinition = sdkgo.TriggerDefinition{
	Trigger: sdkgo.TriggerRef{ConnectorID: "changelog", TriggerName: "itemChanged"}, Description: "Receive a change-log item.",
}

// pollEnvironment is one test's Dex harness, provider, and project storage, kept across simulated restarts.
type pollEnvironment struct {
	harness   *dexHarness
	flow      *pollItemFlow
	objects   *testsupport.ObjectStore
	provider  *changeLogProvider
	logs      *logRecorder
	testRunID string
	mutex     sync.Mutex
	delivered map[string]int
}

func newPollEnvironment(t *testing.T) *pollEnvironment {
	t.Helper()
	flow := &pollItemFlow{}
	environment := &pollEnvironment{
		harness: newDexHarness(t, []dex.Flow{flow}), flow: flow, objects: testsupport.NewObjectStore(),
		provider: newChangeLogProvider(t), logs: newLogRecorder(t),
		testRunID: strconv.FormatInt(time.Now().UnixNano(), 10), delivered: map[string]int{},
	}
	environment.harness.startWorker(t)
	return environment
}

func (environment *pollEnvironment) flowID(event sdkgo.TriggerEvent[pollItem]) string {
	return "poll-trigger-" + environment.testRunID + "-" + strings.ReplaceAll(event.ID, "@", "-")
}

func (environment *pollEnvironment) itemFlowID(itemID string, sequence int) string {
	return environment.flowID(sdkgo.TriggerEvent[pollItem]{ID: itemID + "@" + strconv.Itoa(sequence)})
}

func (environment *pollEnvironment) inboxKey(bindingName string) projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{ConnectorID: "changelog", ConnectionName: pollConnection.Name, TriggerName: pollDefinition.Trigger.TriggerName, BindingName: bindingName}
}

// flowTarget starts the item Flow and counts consumed deliveries per event across processes.
func (environment *pollEnvironment) flowTarget() sdkgo.TriggerTarget[pollItem] {
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow,
		func(sdkgo.TriggerEvent[pollItem]) bool { return true }, environment.flowID,
		func(event sdkgo.TriggerEvent[pollItem]) pollItem { return event.Payload },
		sdkgo.WithTriggerLogger(environment.logs.logger()))
	return sdkgo.TriggerTargetFunc[pollItem](func(ctx context.Context, event sdkgo.TriggerEvent[pollItem]) error {
		err := start.HandleTrigger(ctx, event)
		if err == nil {
			environment.mutex.Lock()
			environment.delivered[event.ID]++
			environment.mutex.Unlock()
		}
		return err
	})
}

func (environment *pollEnvironment) deliveries(eventID string) int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return environment.delivered[eventID]
}

func (environment *pollEnvironment) cursorStore(t *testing.T, bindingName string) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, pollTriggerScope, projectconfig.PollCursorKey(environment.inboxKey(bindingName)))
	require.NoError(t, err)
	return store
}

func (environment *pollEnvironment) cursorState(t *testing.T, bindingName string) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t, bindingName).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

func (environment *pollEnvironment) pendingEvents(t *testing.T, bindingName string) []projectconfig.PendingTriggerEvent {
	t.Helper()
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, pollTriggerScope, environment.inboxKey(bindingName))
	require.NoError(t, err)
	pending, err := inbox.Pending(context.Background())
	require.NoError(t, err)
	return pending
}

// pollProcess is one simulated application process: one Runner.Run over fresh objects.
type pollProcess struct {
	cancel context.CancelFunc
	result chan error
}

type pollProcessOptions struct {
	bindingName string
	pageSize    int
	// wrapCursor intercepts cursor writes, for example to simulate a crash around a commit.
	wrapCursor func(pollingtrigger.CursorStore, context.CancelFunc) pollingtrigger.CursorStore
}

func (environment *pollEnvironment) startProcess(t *testing.T, options pollProcessOptions) *pollProcess {
	t.Helper()
	if options.bindingName == "" {
		options.bindingName = "main"
	}
	ctx, cancel := context.WithCancel(context.Background())
	key := environment.inboxKey(options.bindingName)
	inbox, err := projectconfig.NewTriggerInbox(environment.objects, pollTriggerScope, key)
	require.NoError(t, err)
	durable, err := provider.NewDurableTriggerTarget(inbox, key, environment.flowTarget(), provider.WithTriggerLogger(environment.logs.logger()))
	require.NoError(t, err)
	var cursor pollingtrigger.CursorStore = environment.cursorStore(t, options.bindingName)
	if options.wrapCursor != nil {
		cursor = options.wrapCursor(cursor, cancel)
	}
	poller, err := pollingtrigger.NewBindingPoller(pollingtrigger.BindingPollerConfig[pollItem]{
		Definition: pollDefinition, Binding: sdkgo.TriggerBindingRef{Connection: pollConnection, Trigger: pollDefinition.Trigger, Name: options.bindingName},
		Cursor: cursor, Fetcher: changeLogFetcher{baseURL: environment.provider.server.URL, pageSize: max(options.pageSize, 100)}, Target: durable,
	})
	require.NoError(t, err)
	runner := newPollRunner(t, environment, poller)
	process := &pollProcess{cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { process.stop(t) })
	return process
}

func newPollRunner(t *testing.T, environment *pollEnvironment, pollers ...pollingtrigger.Poller) *pollingtrigger.Runner {
	t.Helper()
	noJitter := 0.0
	runner, err := pollingtrigger.NewRunner(pollingtrigger.RunnerConfig{
		Connection: pollConnection,
		Schedule:   pollingtrigger.PollSchedule{Interval: time.Second, JitterFraction: &noJitter, MaxBackoff: 2 * time.Second},
		Pollers:    pollers, Logger: environment.logs.logger(),
	})
	require.NoError(t, err)
	return runner
}

// stop cancels the process and waits for Run to return; it is idempotent.
func (process *pollProcess) stop(t *testing.T) {
	t.Helper()
	process.cancel()
	select {
	case err, ok := <-process.result:
		if ok {
			require.ErrorIs(t, err, context.Canceled)
			close(process.result)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("poll process did not stop")
	}
}

// commitHookCursor runs hooks around the first commit after the cursor is seeded.
type commitHookCursor struct {
	pollingtrigger.CursorStore
	mutex        sync.Mutex
	hasFired     bool
	beforeCommit func() error
	afterCommit  func()
}

func (cursor *commitHookCursor) ReplacePollCursor(ctx context.Context, revision string, state projectconfig.PollCursorState) (projectconfig.PollCursorRecord, error) {
	cursor.mutex.Lock()
	shouldFire := !cursor.hasFired && state.CommitCount > 1
	cursor.hasFired = cursor.hasFired || shouldFire
	cursor.mutex.Unlock()
	if shouldFire && cursor.beforeCommit != nil {
		if err := cursor.beforeCommit(); err != nil {
			return projectconfig.PollCursorRecord{}, err
		}
	}
	record, err := cursor.CursorStore.ReplacePollCursor(ctx, revision, state)
	if shouldFire && err == nil && cursor.afterCommit != nil {
		cursor.afterCommit()
	}
	return record, err
}

var errSimulatedCrash = errors.New("simulated process crash")

func waitForSeed(t *testing.T, environment *pollEnvironment, bindingName string) {
	t.Helper()
	require.Eventually(t, func() bool { return environment.cursorState(t, bindingName) != nil }, 20*time.Second, 50*time.Millisecond)
}

// seedAndStop runs one process until it seeds the main binding's cursor, then stops it.
func (environment *pollEnvironment) seedAndStop(t *testing.T) {
	t.Helper()
	process := environment.startProcess(t, pollProcessOptions{})
	waitForSeed(t, environment, "main")
	process.stop(t)
}
