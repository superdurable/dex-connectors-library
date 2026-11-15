// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/mondayboardfake"
	"github.com/superdurable/dex-connectors-library/connectors/monday/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "monday-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, apiURL string) Connection {
	t.Helper()
	connection, err := NewConnection(newFetcherTestClient(t, apiURL), fetcherTestConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[ItemChangedEvent]) sdkgo.TriggerTarget[ItemChangedEvent] {
	return sdkgo.TriggerTargetFunc[ItemChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[ItemChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewItemTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[ItemChangedEvent], 1))
	board := ItemChangedTriggerConfiguration{BoardID: fetcherTestBoardID}
	for name, routes := range map[string][]ItemChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target, Configuration: board}},
		"no target":  {{BindingName: "boardChanges", Configuration: board}},
		"no board":   {{BindingName: "boardChanges", Target: target}},
		"duplicate":  {{BindingName: "boardChanges", Target: target, Configuration: board}, {BindingName: "boardChanges", Target: target, Configuration: board}},
	} {
		_, err := NewItemTriggerRunner(ItemTriggerRunnerConfig{Connection: connection, ItemChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewItemTriggerRunner(ItemTriggerRunnerConfig{ItemChangedRoutes: []ItemChangedTriggerRoute{{BindingName: "boardChanges", Target: target, Configuration: board}}})
	require.Error(t, err, "a zero Connection is rejected")
	_, err = New(Config{PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 30s")
}

func TestDurableItemTriggerRunnerRequiresASavedBoard(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	_, err := newDurableItemTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()}, ProjectItemTriggerRunnerConfig{
		ItemChangedRoutes: []ProjectItemChangedTriggerRoute{{BindingName: "boardChanges", Target: channelTarget(nil)}},
	})
	require.ErrorContains(t, err, "has no saved board")
}

func TestDurableItemTriggerRunnerLoadsTheSavedBoardAndCommitsAfterRecording(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	board.Create("5550001", "Existing request")
	connection := newTriggerTestConnection(t, board.URL)
	saved, err := json.Marshal(ItemChangedTriggerConfiguration{BoardID: fetcherTestBoardID})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: fetcherTestConnection.Name, TriggerName: "itemChanged", BindingName: "boardChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[ItemChangedEvent], 4)
	runner, err := newDurableItemTriggerRunner(connection, configuration, storage, ProjectItemTriggerRunnerConfig{
		ItemChangedRoutes: []ProjectItemChangedTriggerRoute{{BindingName: "boardChanges", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: fetcherTestConnection.Name, TriggerName: "itemChanged", BindingName: "boardChanges"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedState := func() *projectconfig.PollCursorState {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil {
			return nil
		}
		return record.State
	}
	require.Eventually(t, func() bool { return committedState() != nil }, 10*time.Second, 10*time.Millisecond)
	baseline := board.UpdatedAt("5550001").Format(time.RFC3339)
	require.JSONEq(t, `{"boardId":"1234567890","watermark":"`+baseline+`"}`, committedState().Token)
	board.Create("5550002", "New request")
	runner.WakePoller("boardChanges")

	select {
	case event := <-events:
		require.Equal(t, "5550002", event.Payload.Item.ID, "the baseline item is never delivered")
		require.Equal(t, ItemChangeCreated, event.Payload.ChangeKind)
	case <-time.After(10 * time.Second):
		t.Fatal("the new item was not delivered")
	}
	latest := board.UpdatedAt("5550002").Format(time.RFC3339)
	require.Eventually(t, func() bool {
		state := committedState()
		return state != nil && state.Token == `{"boardId":"1234567890","watermark":"`+latest+`"}` && state.Snapshot != nil && len(state.Snapshot.Entries) == 2
	}, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedItemChangedTriggerRecordsABaselineOnStart(t *testing.T) {
	board := mondayboardfake.New(t, fetcherTestBoardID)
	board.Create("5550001", "Existing request")
	trigger := NewItemChangedTrigger(ItemChangedTriggerConfig{
		Connection: newTriggerTestConnection(t, board.URL), ConnectionName: fetcherTestConnection.Name, BindingName: "boardChanges",
		Configuration: ItemChangedTriggerConfiguration{BoardID: fetcherTestBoardID}, Target: channelTarget(make(chan sdkgo.TriggerEvent[ItemChangedEvent], 1)),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(board.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, "ListBoardItems", board.Requests()[0].Operation)
	require.Panics(t, func() {
		NewItemChangedTrigger(ItemChangedTriggerConfig{
			Connection: newTriggerTestConnection(t, board.URL), ConnectionName: fetcherTestConnection.Name, BindingName: "boardChanges",
			Target: channelTarget(nil),
		})
	}, "a binding without a board ID is a configuration fault")
}
