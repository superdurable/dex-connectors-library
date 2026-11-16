// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go/internal/sheetsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// recordingRowTarget collects delivered row numbers.
type recordingRowTarget struct {
	mutex sync.Mutex
	rows  []int64
}

func (target *recordingRowTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[RowAddedEvent]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	target.rows = append(target.rows, event.Payload.RowNumber)
	return nil
}

func (target *recordingRowTarget) deliveredRows() []int64 {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	return slices.Clone(target.rows)
}

func newRowAddedTestConnection(t *testing.T, endpoint string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "google", Name: "sheets-leads"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("SENTINEL-ACCESS-TOKEN")},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

// runUntil runs run in the background and cancels it once condition holds.
func runUntil(t *testing.T, run func(context.Context) error, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- run(ctx) }()
	require.Eventually(t, condition, 20*time.Second, 20*time.Millisecond)
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
}

func TestGeneratedRowAddedTriggerSeedsThenDeliversAppendedRows(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads", leadsHeader, []string{"existing"})
	connection := newRowAddedTestConnection(t, sheet.URL)
	target := &recordingRowTarget{}
	trigger := NewRowAddedTrigger(RowAddedTriggerConfig{
		Connection: connection, ConnectionName: "sheets-leads", BindingName: "new-leads",
		Configuration: RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1", SheetName: "Leads"}, Target: target,
	})
	sheet.AppendRowsAfterRequests(1, []string{"added"})
	runUntil(t, trigger.Run, func() bool { return len(target.deliveredRows()) == 1 })
	require.Equal(t, []int64{3}, target.deliveredRows())

	invalid := NewRowAddedTrigger(RowAddedTriggerConfig{
		Connection: connection, ConnectionName: "sheets-leads", BindingName: "new-leads", Target: target,
	})
	require.ErrorContains(t, invalid.Run(context.Background()), "spreadsheetId and sheetName are required")
}

func TestRowAddedTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRowAddedTestConnection(t, "https://sheets.example.test")
	valid := RowAddedTriggerRoute{
		BindingName: "new-leads", Configuration: RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1", SheetName: "Leads"},
		Cursor: pollingtrigger.NewMemoryCursorStore(), Target: &recordingRowTarget{},
	}
	_, err := NewRowAddedTriggerRunner(RowAddedTriggerRunnerConfig{Connection: connection, Routes: []RowAddedTriggerRoute{valid}})
	require.NoError(t, err)
	for name, route := range map[string]RowAddedTriggerRoute{
		"binding":       {Configuration: valid.Configuration, Cursor: valid.Cursor, Target: valid.Target},
		"cursor":        {BindingName: "new-leads", Configuration: valid.Configuration, Target: valid.Target},
		"target":        {BindingName: "new-leads", Configuration: valid.Configuration, Cursor: valid.Cursor},
		"configuration": {BindingName: "new-leads", Cursor: valid.Cursor, Target: valid.Target},
	} {
		_, err := NewRowAddedTriggerRunner(RowAddedTriggerRunnerConfig{Connection: connection, Routes: []RowAddedTriggerRoute{route}})
		require.Error(t, err, name)
	}
	duplicate := valid
	duplicate.Cursor = pollingtrigger.NewMemoryCursorStore()
	_, err = NewRowAddedTriggerRunner(RowAddedTriggerRunnerConfig{Connection: connection, Routes: []RowAddedTriggerRoute{valid, duplicate}})
	require.Error(t, err)
	_, err = NewRowAddedTriggerRunner(RowAddedTriggerRunnerConfig{Connection: connection})
	require.Error(t, err)
	_, err = NewRowAddedTriggerRunner(RowAddedTriggerRunnerConfig{Routes: []RowAddedTriggerRoute{valid}})
	require.Error(t, err)
}

func TestDurableRowAddedTriggerRunnerKeepsItsCursorAcrossRestarts(t *testing.T) {
	sheet := sheetsfake.New(t, "Leads", leadsHeader, []string{"existing"})
	connection := newRowAddedTestConnection(t, sheet.URL)
	store := projectstoretest.New()
	bindingConfiguration, err := json.Marshal(RowAddedTriggerConfiguration{SpreadsheetID: "sheet-1", SheetName: "Leads"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "sheets-leads", TriggerName: "rowAdded", BindingName: "new-leads", Configuration: bindingConfiguration,
	}}}
	openCursor := func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) { return store.Cursor(key) }
	target := &recordingRowTarget{}
	newRunner := func() *RowAddedTriggerRunner {
		runner, err := newDurableRowAddedTriggerRunner(connection, configuration, store.Inbox, openCursor,
			[]ProjectRowAddedTriggerRoute{{BindingName: "new-leads", Target: target}})
		require.NoError(t, err)
		return runner
	}
	sheet.AppendRowsAfterRequests(1, []string{"first"})
	runUntil(t, newRunner().Run, func() bool { return len(target.deliveredRows()) == 1 })
	// Rows added while no runner polls are delivered after the restart; nothing is seeded or replayed.
	sheet.AppendRows([]string{"while-stopped-1"}, []string{"while-stopped-2"})
	runUntil(t, newRunner().Run, func() bool { return len(target.deliveredRows()) == 3 })
	require.Equal(t, []int64{3, 4, 5}, target.deliveredRows())
	pending, err := store.PendingEventIDs(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = newDurableRowAddedTriggerRunner(connection, configuration, store.Inbox, openCursor,
		[]ProjectRowAddedTriggerRoute{{BindingName: "unsaved-binding", Target: target}})
	require.ErrorContains(t, err, "unsaved-binding")
}
