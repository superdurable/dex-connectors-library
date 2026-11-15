// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go/internal/notionrowsfake"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var rowTriggerTestScope = projectconfig.Scope{ProjectID: "notion-row-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, rowTriggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, rowTriggerTestScope, key)
}

func newRowTriggerTestConnection(t *testing.T, endpoint string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "notion", Name: "rows"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIToken: sdkgo.NewSecretString(notionrowsfake.Token)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func rowChannelTarget(events chan sdkgo.TriggerEvent[DatabaseRowChangedEvent]) sdkgo.TriggerTarget[DatabaseRowChangedEvent] {
	return sdkgo.TriggerTargetFunc[DatabaseRowChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[DatabaseRowChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewDatabaseRowTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRowTriggerTestConnection(t, "http://127.0.0.1:1")
	target := rowChannelTarget(make(chan sdkgo.TriggerEvent[DatabaseRowChangedEvent], 1))
	watched := DatabaseRowChangedTriggerConfiguration{DataSourceID: rowsDataSourceID}
	for name, routes := range map[string][]DatabaseRowChangedTriggerRoute{
		"none":           nil,
		"no binding":     {{Target: target, Configuration: watched}},
		"no target":      {{BindingName: "rowChanges", Configuration: watched}},
		"no data source": {{BindingName: "rowChanges", Target: target}},
		"duplicate":      {{BindingName: "rowChanges", Target: target, Configuration: watched}, {BindingName: "rowChanges", Target: target, Configuration: watched}},
	} {
		_, err := NewDatabaseRowTriggerRunner(DatabaseRowTriggerRunnerConfig{Connection: connection, DatabaseRowChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewDatabaseRowTriggerRunner(DatabaseRowTriggerRunnerConfig{DatabaseRowChangedRoutes: []DatabaseRowChangedTriggerRoute{{BindingName: "rowChanges", Target: target, Configuration: watched}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableDatabaseRowTriggerRunnerRequiresASavedDataSource(t *testing.T) {
	connection := newRowTriggerTestConnection(t, "http://127.0.0.1:1")
	_, err := newDurableDatabaseRowTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()},
		ProjectDatabaseRowTriggerRunnerConfig{DatabaseRowChangedRoutes: []ProjectDatabaseRowChangedTriggerRoute{{
			BindingName: "rowChanges", Target: rowChannelTarget(make(chan sdkgo.TriggerEvent[DatabaseRowChangedEvent], 1)),
		}}})
	require.ErrorContains(t, err, "has no saved data source; pick one in Dex Web")
}

func TestDurableDatabaseRowTriggerRunnerLoadsTheSavedDataSourceAndCommitsAfterRecording(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	now := time.Now().UTC().Truncate(time.Minute)
	fake.Create(rowA, "Existing", now.Add(-time.Hour))
	connection := newRowTriggerTestConnection(t, fake.URL)
	saved, err := json.Marshal(DatabaseRowChangedTriggerConfiguration{DataSourceID: strings.ReplaceAll(rowsDataSourceID, "-", ""), DataSourceName: "Requests"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "rows", TriggerName: "databaseRowChanged", BindingName: "rowChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[DatabaseRowChangedEvent], 4)
	runner, err := newDurableDatabaseRowTriggerRunner(connection, configuration, storage, ProjectDatabaseRowTriggerRunnerConfig{
		DatabaseRowChangedRoutes: []ProjectDatabaseRowChangedTriggerRoute{{BindingName: "rowChanges", Target: rowChannelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "rows", TriggerName: "databaseRowChanged", BindingName: "rowChanges"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedPosition := func() databaseRowCursor {
		record, err := cursor.ReadPollCursor(context.Background())
		var position databaseRowCursor
		if err != nil || record.State == nil || json.Unmarshal([]byte(record.State.Token), &position) != nil {
			return databaseRowCursor{}
		}
		return position
	}
	require.Eventually(t, func() bool { return !committedPosition().HighWater.IsZero() }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, rowsDataSourceID, committedPosition().DataSourceID, "the saved undashed ID is watched in its dashed form")
	require.Equal(t, now.Add(-time.Hour), committedPosition().HighWater)
	fake.Create(rowB, "Kickoff", now)
	runner.WakePoller("rowChanges")

	select {
	case event := <-events:
		require.Equal(t, rowB, event.Payload.Row.ID, "the baseline row is never delivered")
		require.Equal(t, DatabaseRowCreated, event.Payload.ChangeKind)
	case <-time.After(10 * time.Second):
		t.Fatal("the new row was not delivered")
	}
	require.Eventually(t, func() bool { return committedPosition().HighWater.Equal(now) }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedDatabaseRowChangedTriggerBaselinesAndDeliversTheNextEdit(t *testing.T) {
	fake := notionrowsfake.New(t, rowsDataSourceID, rowsDatabaseID)
	now := time.Now().UTC().Truncate(time.Minute)
	fake.Create(rowA, "Existing", now.Add(-time.Hour))
	connection := newRowTriggerTestConnection(t, fake.URL)
	events := make(chan sdkgo.TriggerEvent[DatabaseRowChangedEvent], 4)
	trigger := NewDatabaseRowChangedTrigger(DatabaseRowChangedTriggerConfig{
		Connection: connection, ConnectionName: "rows", BindingName: "rowChanges", Target: rowChannelTarget(events),
		Configuration: DatabaseRowChangedTriggerConfiguration{DataSourceID: rowsDataSourceID},
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.Requests()) >= 2 }, 10*time.Second, 10*time.Millisecond)
	fake.Edit(rowA, "Existing edited", now)

	select {
	case event := <-events:
		require.Equal(t, DatabaseRowUpdated, event.Payload.ChangeKind)
		require.Equal(t, "Existing edited", event.Payload.Row.Title)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the edit after one poll interval")
	}
}
