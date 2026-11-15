//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recordchanges

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	salesforce "github.com/superdurable/dex-connectors-library/connectors/salesforce/go"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/internal/salesforcepollfake"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig/provider"
	"github.com/superdurable/dex/sdk-go/dex"
)

const integrationToken = "salesforce-integration-token"

var integrationScope = projectconfig.Scope{ProjectID: "salesforce-record-changes", Kind: "live"}

// TestRecordChangesStartOneFlowEachAcrossARestartWithRealDex: existing records are a baseline, each later
// change starts exactly one Flow, a bulk load sharing one SystemModstamp pages by Id, a rate limit pauses
// the poll without losing changes, and changes made while the process is stopped arrive after a restart.
func TestRecordChangesStartOneFlowEachAcrossARestartWithRealDex(t *testing.T) {
	environment := newRecordChangesEnvironment(t)
	ctx := integrationContext(t, 3*time.Minute)
	renewal := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "Renewal", "StageName": "Prospecting"})
	doomed := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "Doomed", "StageName": "Prospecting"})
	first := environment.startTriggerProcess(t)
	environment.waitForCommittedRecord(t, doomed)

	expansion := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "Expansion", "StageName": "Qualification"})
	environment.org.Update(renewal, map[string]any{"StageName": "Closed Won"})
	environment.org.Update(doomed, map[string]any{"StageName": "Closed Lost"})
	environment.deleteBeforeStart(doomed)
	first.runner.WakePoller(RecordModifiedTriggerBinding)
	outcomes := environment.waitForOutcomes(ctx, t, 3)
	require.Equal(t, salesforce.RecordChangeCreated, outcomes[expansion].ChangeKind)
	require.Equal(t, "Expansion", stringField(t, outcomes[expansion].Current, "Name"))
	require.Equal(t, salesforce.RecordChangeUpdated, outcomes[renewal].ChangeKind)
	require.Equal(t, "Closed Won", stringField(t, outcomes[renewal].Current, "StageName"), "the Flow reads the record back")
	require.True(t, outcomes[doomed].IsDeleted, "a record deleted before the read-back completes as deleted")
	require.Nil(t, outcomes[doomed].Current)

	bulk := environment.org.InsertBatch(environment.sObjectType, 5, func(index int) map[string]any {
		return map[string]any{"Name": "Bulk " + strconv.Itoa(index), "StageName": "Prospecting"}
	})
	environment.org.RateLimitNext(1, "1")
	first.runner.WakePoller(RecordModifiedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 8)
	for _, id := range bulk {
		require.Equal(t, salesforce.RecordChangeCreated, outcomes[id].ChangeKind, "every record of a bulk load sharing one stamp arrives")
	}
	first.stop(t)

	offline := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "Created while stopped"})
	environment.org.Update(expansion, map[string]any{"StageName": "Negotiation"})
	second := environment.startTriggerProcess(t)
	second.runner.WakePoller(RecordModifiedTriggerBinding)
	outcomes = environment.waitForOutcomes(ctx, t, 10)
	require.Equal(t, "Created while stopped", stringField(t, outcomes[offline].Current, "Name"))
	require.Equal(t, "Negotiation", stringField(t, outcomes[expansion].Current, "StageName"))
	environment.waitForCommittedRecord(t, expansion)
	require.Equal(t, 10, environment.deliveryCount(), "no change is delivered twice and the baseline is never delivered")
	require.Zero(t, environment.cursorState(t).ResetCount)
}

// TestChangedObjectRecordsANewBaselineWithRealDex: a cursor for another object is invalid, so the poll
// reseeds without delivering the new object's existing records.
func TestChangedObjectRecordsANewBaselineWithRealDex(t *testing.T) {
	environment := newRecordChangesEnvironment(t)
	ctx := integrationContext(t, 2*time.Minute)
	environment.org.Insert(environment.sObjectType, map[string]any{"Name": "First object"})
	first := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool { return environment.cursorState(t) != nil }, 30*time.Second, 50*time.Millisecond)
	first.stop(t)

	environment.sObjectType = "Other" + environment.runID + "__c"
	environment.configuration.SObjectType = environment.sObjectType
	existing := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "Already there"})
	second := environment.startTriggerProcess(t)
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		return state != nil && state.ResetCount == 1
	}, 30*time.Second, 50*time.Millisecond)
	environment.waitForCommittedRecord(t, existing)
	afterReset := environment.org.Insert(environment.sObjectType, map[string]any{"Name": "After the reset"})
	second.runner.WakePoller(RecordModifiedTriggerBinding)

	outcomes := environment.waitForOutcomes(ctx, t, 1)
	require.Equal(t, "After the reset", stringField(t, outcomes[afterReset].Current, "Name"))
	require.NotContains(t, outcomes, existing, "the new object's existing records are its baseline")
}

type recordChangesEnvironment struct {
	harness       *dexHarness
	flow          *Flow
	org           *salesforcepollfake.Org
	connection    salesforce.Connection
	objects       *testsupport.ObjectStore
	runID         string
	sObjectType   string
	configuration salesforce.RecordModifiedTriggerConfiguration
	mutex         sync.Mutex
	delivered     map[string]sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]
	order         []string
	deleteOnStart map[string]bool
}

// newRecordChangesEnvironment watches a custom object unique to the run, so Flow IDs are unique per run.
func newRecordChangesEnvironment(t *testing.T) *recordChangesEnvironment {
	t.Helper()
	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	org := salesforcepollfake.New(t, integrationToken, nil)
	reference := sdkgo.ConnectionRef{Provider: "salesforce", Name: ConnectionName}
	client, err := salesforce.New(salesforce.Config{PollInterval: 5 * time.Second, PollPageSize: 2}, sdkgo.StaticCredentialProvider[salesforce.Credentials]{
		reference: {AuthMethodID: salesforce.ProductionOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(integrationToken), InstanceURL: org.URL},
	})
	require.NoError(t, err)
	connection, err := salesforce.NewConnection(client, reference)
	require.NoError(t, err)
	flow := NewFlow(connection)
	sObjectType := "Deal" + runID + "__c"
	return &recordChangesEnvironment{
		harness: newDexHarness(t, flow), flow: flow, org: org, connection: connection, objects: testsupport.NewObjectStore(),
		runID: runID, sObjectType: sObjectType,
		configuration: salesforce.RecordModifiedTriggerConfiguration{SObjectType: sObjectType, FieldNames: []string{"Name", "StageName"}},
		delivered:     map[string]sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]{}, deleteOnStart: map[string]bool{},
	}
}

func (environment *recordChangesEnvironment) inboxKey() projectconfig.TriggerInboxKey {
	return projectconfig.TriggerInboxKey{
		ConnectorID: salesforce.ConnectorID, ConnectionName: ConnectionName,
		TriggerName: salesforce.RecordModifiedPolledTriggerDefinition.Trigger.TriggerName, BindingName: RecordModifiedTriggerBinding,
	}
}

// deleteBeforeStart deletes recordID in the fake org just before its change starts a Flow.
func (environment *recordChangesEnvironment) deleteBeforeStart(recordID string) {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	environment.deleteOnStart[recordID] = true
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
// salesforce.NewProjectRecordTriggerRunner does, over storage that survives the process.
func (environment *recordChangesEnvironment) startTriggerProcess(t *testing.T) *triggerProcess {
	t.Helper()
	start := sdkgo.NewDexFlowTriggerTarget(environment.harness.client, environment.flow, AcceptEveryRecordChange, ResolveFlowID, MapToFlowInput)
	counting := sdkgo.TriggerTargetFunc[salesforce.RecordModifiedEvent](func(ctx context.Context, event sdkgo.TriggerEvent[salesforce.RecordModifiedEvent]) error {
		environment.mutex.Lock()
		if environment.deleteOnStart[event.Payload.RecordID] {
			environment.org.Delete(event.Payload.RecordID)
		}
		environment.mutex.Unlock()
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
	target, err := provider.NewDurableTriggerTarget(inbox, key, sdkgo.TriggerTarget[salesforce.RecordModifiedEvent](counting))
	require.NoError(t, err)
	runner, err := salesforce.NewRecordTriggerRunner(salesforce.RecordTriggerRunnerConfig{
		Connection: environment.connection,
		RecordModifiedPolledRoutes: []salesforce.RecordModifiedPolledTriggerRoute{{
			BindingName: RecordModifiedTriggerBinding, Configuration: environment.configuration,
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

func (environment *recordChangesEnvironment) cursorStore(t *testing.T) *projectconfig.PollCursorStore {
	t.Helper()
	store, err := projectconfig.NewPollCursorStore(environment.objects, integrationScope, projectconfig.PollCursorKey(environment.inboxKey()))
	require.NoError(t, err)
	return store
}

func (environment *recordChangesEnvironment) cursorState(t *testing.T) *projectconfig.PollCursorState {
	t.Helper()
	record, err := environment.cursorStore(t).ReadPollCursor(context.Background())
	require.NoError(t, err)
	return record.State
}

// waitForCommittedRecord waits until the committed cursor sits on recordID of the watched object.
func (environment *recordChangesEnvironment) waitForCommittedRecord(t *testing.T, recordID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		state := environment.cursorState(t)
		var position struct {
			SObjectType string `json:"sObjectType"`
			RecordID    string `json:"recordId"`
		}
		return state != nil && json.Unmarshal([]byte(state.Token), &position) == nil &&
			position.SObjectType == environment.sObjectType && position.RecordID == recordID
	}, 30*time.Second, 50*time.Millisecond)
}

func (environment *recordChangesEnvironment) deliveryCount() int {
	environment.mutex.Lock()
	defer environment.mutex.Unlock()
	return len(environment.delivered)
}

// waitForOutcomes waits for count delivered changes, then for each change's Flow, keyed by record ID; a
// later change to the same record replaces the earlier outcome.
func (environment *recordChangesEnvironment) waitForOutcomes(ctx context.Context, t *testing.T, count int) map[string]ChangeOutcome {
	t.Helper()
	require.Eventually(t, func() bool { return environment.deliveryCount() >= count }, 60*time.Second, 50*time.Millisecond)
	environment.mutex.Lock()
	events := make([]sdkgo.TriggerEvent[salesforce.RecordModifiedEvent], 0, len(environment.order))
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
		require.Equal(t, event.Payload.RecordID, outcome.RecordID)
		outcomes[outcome.RecordID] = outcome
	}
	return outcomes
}

func stringField(t *testing.T, record *salesforce.Record, fieldName string) string {
	t.Helper()
	require.NotNil(t, record)
	value, isPresent := record.StringField(fieldName)
	require.True(t, isPresent, fieldName)
	return value
}
