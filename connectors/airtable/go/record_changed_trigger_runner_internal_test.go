// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/airtablefake"
	"github.com/superdurable/dex-connectors-library/connectors/airtable/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// recordingChangeTarget collects delivered record IDs.
type recordingChangeTarget struct {
	mutex     sync.Mutex
	recordIDs []string
}

func (target *recordingChangeTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[RecordChangedEvent]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	target.recordIDs = append(target.recordIDs, event.Payload.Record.ID)
	return nil
}

func (target *recordingChangeTarget) deliveredRecordIDs() []string {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	return slices.Clone(target.recordIDs)
}

func newRecordChangedTestConnection(t *testing.T, endpoint string) Connection {
	t.Helper()
	client, err := New(Config{Endpoint: endpoint, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{
		fakeConnection: {PersonalAccessToken: sdkgo.NewSecretString(airtablefake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, fakeConnection)
	require.NoError(t, err)
	return connection
}

// runUntil runs run in the background, calls afterSeed once the seed requests completed, and cancels
// run once condition holds.
func runUntil(t *testing.T, table *airtablefake.Table, run func(context.Context) error, afterSeed func(), condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	requestsBefore := table.CompletedRequests()
	go func() { result <- run(ctx) }()
	require.Eventually(t, func() bool { return table.CompletedRequests() >= requestsBefore+2 }, 20*time.Second, 10*time.Millisecond)
	afterSeed()
	require.Eventually(t, condition, 20*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestGeneratedRecordChangedTriggerSeedsThenDeliversChanges(t *testing.T) {
	table := airtablefake.New(t, fakeRecord("recExisting000001", seedTime))
	connection := newRecordChangedTestConnection(t, table.URL)
	target := &recordingChangeTarget{}
	trigger := NewRecordChangedTrigger(RecordChangedTriggerConfig{
		Connection: connection, ConnectionName: fakeConnection.Name, BindingName: "lead-changed", Configuration: fakeTrigger, Target: target,
	})
	runUntil(t, table, trigger.Run, func() { table.Put(fakeRecord("recNewLead0000001", seedTime.Add(time.Second))) },
		func() bool { return len(target.deliveredRecordIDs()) == 1 })
	require.Equal(t, []string{"recNewLead0000001"}, target.deliveredRecordIDs())

	invalid := NewRecordChangedTrigger(RecordChangedTriggerConfig{
		Connection: connection, ConnectionName: fakeConnection.Name, BindingName: "lead-changed", Target: target,
	})
	require.ErrorContains(t, invalid.Run(context.Background()), "baseId")
}

func TestRecordChangedTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRecordChangedTestConnection(t, "https://api.airtable.com")
	valid := RecordChangedTriggerRoute{
		BindingName: "lead-changed", Configuration: fakeTrigger, Cursor: pollingtrigger.NewMemoryCursorStore(), Target: &recordingChangeTarget{},
	}
	_, err := NewRecordChangedTriggerRunner(RecordChangedTriggerRunnerConfig{Connection: connection, Routes: []RecordChangedTriggerRoute{valid}})
	require.NoError(t, err)
	for name, route := range map[string]RecordChangedTriggerRoute{
		"binding":       {Configuration: valid.Configuration, Cursor: valid.Cursor, Target: valid.Target},
		"cursor":        {BindingName: "lead-changed", Configuration: valid.Configuration, Target: valid.Target},
		"target":        {BindingName: "lead-changed", Configuration: valid.Configuration, Cursor: valid.Cursor},
		"configuration": {BindingName: "lead-changed", Cursor: valid.Cursor, Target: valid.Target},
	} {
		_, err := NewRecordChangedTriggerRunner(RecordChangedTriggerRunnerConfig{Connection: connection, Routes: []RecordChangedTriggerRoute{route}})
		require.Error(t, err, name)
	}
	duplicate := valid
	duplicate.Cursor = pollingtrigger.NewMemoryCursorStore()
	_, err = NewRecordChangedTriggerRunner(RecordChangedTriggerRunnerConfig{Connection: connection, Routes: []RecordChangedTriggerRoute{valid, duplicate}})
	require.Error(t, err)
	_, err = NewRecordChangedTriggerRunner(RecordChangedTriggerRunnerConfig{Connection: connection})
	require.Error(t, err)
	_, err = NewRecordChangedTriggerRunner(RecordChangedTriggerRunnerConfig{Routes: []RecordChangedTriggerRoute{valid}})
	require.Error(t, err)
}

func TestDurableRecordChangedTriggerRunnerKeepsItsCursorAcrossRestarts(t *testing.T) {
	table := airtablefake.New(t, fakeRecord("recExisting000001", seedTime))
	connection := newRecordChangedTestConnection(t, table.URL)
	store := projectstoretest.New()
	bindingConfiguration, err := json.Marshal(fakeTrigger)
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: fakeConnection.Name, TriggerName: "recordChanged", BindingName: "lead-changed", Configuration: bindingConfiguration,
	}}}
	openCursor := func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) { return store.Cursor(key) }
	target := &recordingChangeTarget{}
	newRunner := func() *RecordChangedTriggerRunner {
		runner, err := newDurableRecordChangedTriggerRunner(connection, configuration, store.Inbox, openCursor,
			[]ProjectRecordChangedTriggerRoute{{BindingName: "lead-changed", Target: target}})
		require.NoError(t, err)
		return runner
	}
	runUntil(t, table, newRunner().Run, func() { table.Put(fakeRecord("recFirst000000001", seedTime.Add(time.Second))) },
		func() bool { return len(target.deliveredRecordIDs()) == 1 })
	// Changes made while no runner polls are delivered after the restart; nothing is seeded or replayed.
	table.Put(fakeRecord("recStopped0000001", seedTime.Add(2*time.Second)), fakeRecord("recExisting000001", seedTime.Add(3*time.Second)))
	runUntil(t, table, newRunner().Run, func() {}, func() bool { return len(target.deliveredRecordIDs()) == 3 })
	require.Equal(t, []string{"recFirst000000001", "recStopped0000001", "recExisting000001"}, target.deliveredRecordIDs())
	pending, err := store.PendingEventIDs(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = newDurableRecordChangedTriggerRunner(connection, configuration, store.Inbox, openCursor,
		[]ProjectRecordChangedTriggerRoute{{BindingName: "unsaved-binding", Target: target}})
	require.ErrorContains(t, err, "unsaved-binding")
}
