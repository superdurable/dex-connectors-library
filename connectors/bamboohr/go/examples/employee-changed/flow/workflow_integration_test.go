//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package employeechanged

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go/internal/bamboohrfake"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/blob-cache-go/blobcache"
	"github.com/superdurable/dex/sdk-go/dex"
)

var integrationScope = projectconfig.Scope{ProjectID: "bamboohr-employee-changed", Kind: "live"}

// TestEmployeeChangesStartOneFlowEachAcrossARestartWithRealDex: existing employees are never delivered,
// each later change starts exactly one Flow that reads the employee back, and changes made while the
// process is stopped arrive after a restart whose first change-list request is rate limited.
func TestEmployeeChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newEmployeeChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Insert("140", map[string]string{"firstName": "Ava", "lastName": "Nguyen", "status": "Active"})
	environment.provider.Insert("150", map[string]string{"firstName": "Lee", "lastName": "Park", "status": "Active"})
	environment.provider.Advance(time.Second)
	first := environment.startTriggerProcess(t)
	environment.waitForCursorAt(t, environment.provider.Clock())

	environment.provider.Insert("141", map[string]string{"firstName": "Sam", "lastName": "Park", "workEmail": "sam.park@example.com", "status": "Active"})
	environment.provider.Update("140", map[string]string{"status": "Inactive", "terminationDate": "2026-10-31"})
	environment.provider.Delete("150")
	environment.provider.Advance(30 * time.Second) // The connector delivers a change once it is 30s old.
	first.runner.WakePoller(EmployeeChangedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, bamboohr.EmployeeChangeActionInserted, outcomes["141"].Action)
	require.Equal(t, "sam.park@example.com", outcomes["141"].Current.Fields["workEmail"])
	require.Equal(t, bamboohr.EmployeeChangeActionUpdated, outcomes["140"].Action, "a termination is an update")
	require.Equal(t, "2026-10-31", outcomes["140"].Current.Fields["terminationDate"], "the Flow reads the employee back from BambooHR")
	require.Equal(t, bamboohr.EmployeeChangeActionDeleted, outcomes["150"].Action)
	require.True(t, outcomes["150"].IsGone)
	require.Nil(t, outcomes["150"].Current, "a deleted employee is not read back")
	first.stop(t)

	environment.provider.Insert("142", map[string]string{"firstName": "Kai", "lastName": "Lin", "status": "Active"})
	environment.provider.Update("141", map[string]string{"department": "Sales"})
	environment.provider.Advance(30 * time.Second)
	environment.provider.RateLimitChangeLists(1)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(EmployeeChangedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 5)
	require.Equal(t, "Kai", outcomes["142"].Current.Fields["firstName"])
	require.Equal(t, bamboohr.EmployeeChangeActionUpdated, outcomes["141"].Action)
	require.Equal(t, "Sales", outcomes["141"].Current.Fields["department"])
	environment.waitForCursorAt(t, environment.provider.LatestChange())
	require.Equal(t, 5, environment.deliveryCount(), "no change is delivered twice and existing employees are never delivered")
}

// TestRejectedCursorStartsAgainAtTheCurrentTimeWithRealDex: a 400 for the stored cursor reseeds without
// delivering the gap, and the next change is delivered.
func TestRejectedCursorStartsAgainAtTheCurrentTimeWithRealDex(t *testing.T) {
	environment := newEmployeeChangedEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.provider.Insert("140", map[string]string{"firstName": "Ava"})
	environment.provider.Advance(time.Second)
	first := environment.startTriggerProcess(t)
	environment.waitForCursorAt(t, environment.provider.Clock())
	first.stop(t)

	environment.provider.Insert("160", map[string]string{"firstName": "Gap"})
	environment.provider.Advance(time.Second)
	environment.provider.RejectChangeLists(1)
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(EmployeeChangedTriggerBinding)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForCursorAt(t, environment.provider.Clock())
	environment.provider.Insert("161", map[string]string{"firstName": "After"})
	environment.provider.Advance(30 * time.Second)
	second.runner.WakePoller(EmployeeChangedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After", outcomes["161"].Current.Fields["firstName"])
	require.NotContains(t, outcomes, "160", "changes between the rejected cursor and the reseed are not delivered")
}

type employeeChangedEnvironment struct {
	harness    *dexHarness
	flow       *Flow
	provider   *bamboohrfake.BambooHR
	connection bamboohr.Connection
	objects    *testsupport.ObjectStore
	mutex      sync.Mutex
	delivered  map[string]sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]
	order      []string
}

func newEmployeeChangedEnvironment(t *testing.T) *employeeChangedEnvironment {
	t.Helper()
	// A unique provider clock keeps event IDs, and so Flow IDs, unique on a shared Dex server.
	fake := bamboohrfake.NewAt(t, time.Now().Add(-time.Hour))
	reference := sdkgo.ConnectionRef{Provider: "bamboohr", Name: ConnectionName}
	client, err := bamboohr.New(bamboohr.Config{CompanyDomain: "acme", PollInterval: 10 * time.Second},
		sdkgo.StaticCredentialProvider[bamboohr.Credentials]{reference: {APIKey: sdkgo.NewSecretString(bamboohrfake.APIKey)}},
		bamboohr.WithAPIBaseURL(fake.APIBaseURL()))
	require.NoError(t, err)
	connection, err := bamboohr.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	return &employeeChangedEnvironment{
		harness: newDexHarness(t, flow), flow: flow, provider: fake, connection: connection,
		objects: testsupport.NewObjectStore(), delivered: map[string]sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]{},
	}
}

func (environment *employeeChangedEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: bamboohr.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: bamboohr.EmployeeChangedTriggerDefinition.Trigger.TriggerName, BindingName: EmployeeChangedTriggerBinding,
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
// bamboohr.NewProjectEmployeeChangedTriggerRunner does, over storage that survives the process.
func (environment *employeeChangedEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryEmployeeChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[bamboohr.EmployeeChangedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent]) error {
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[bamboohr.EmployeeChangedEvent](counting))
	require.NoError(t, err)
	runner, err := bamboohr.NewEmployeeChangedTriggerRunner(bamboohr.EmployeeChangedTriggerRunnerConfig{
		Connection: environment.connection,
		Routes: []bamboohr.EmployeeChangedTriggerRoute{{
			BindingName: EmployeeChangedTriggerBinding, Target: target, Cursor: environment.cursorStore(t),
		}},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	process := &triggerProcess{runner: runner, cancel: cancel, result: make(chan error, 1)}
	go func() { process.result <- runner.Run(ctx) }()
	t.Cleanup(func() { _ = process.stopAndWait() }) // The test body asserts the stops it cares about.
	return process
}

func (environment *employeeChangedEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *employeeChangedEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCursorAt waits until the committed cursor stands at since.
func (environment *employeeChangedEnvironment) waitForCursorAt(t *testing.T, since time.Time) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		if state == nil {
			return false
		}
		var position struct {
			Since time.Time `json:"since"`
		}
		return json.Unmarshal([]byte(state.Token), &position) == nil && position.Since.Equal(since)
	}, 45*time.Second, 50*time.Millisecond)
}

func (environment *employeeChangedEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by employee ID; a
// later change to the same employee replaces the earlier outcome.
func (environment *employeeChangedEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 45*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[bamboohr.EmployeeChangedEvent], 0, len(environment.order))
	for _, eventID := range environment.order {
		events = append(events, environment.delivered[eventID])
	}
	environment.mutex.Unlock()
	outcomes := map[string]ChangeOutcome{}
	for _, event := range events {
		require.True(t, strings.HasPrefix(event.ID, "employee-"+event.Payload.EmployeeID+"-"))
		result, err := environment.harness.client.WaitForFlow(ctx, ResolveFlowID(event), dex.WaitForFlowOptions{NeedsResults: true})
		require.NoError(t, err)
		require.Equal(t, dex.FlowCompleted, result.Status, "flow %s: %s", ResolveFlowID(event), result.ErrorMessage)
		var outcome ChangeOutcome
		require.NoError(t, result.DecodeSingleOutput(&outcome))
		require.Equal(t, event.Payload.EmployeeID, outcome.EmployeeID)
		outcomes[outcome.EmployeeID] = outcome
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
