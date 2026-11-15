// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/trello/go/internal/trelloboardfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "trello-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, endpoint string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "trello", Name: "trello-cards"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {APIKey: sdkgo.NewSecretString(trelloboardfake.APIKey), Token: sdkgo.NewSecretString(trelloboardfake.Token)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[CardChangedEvent]) sdkgo.TriggerTarget[CardChangedEvent] {
	return sdkgo.TriggerTargetFunc[CardChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[CardChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewClientRejectsAPollIntervalUnderTenSeconds(t *testing.T) {
	_, err := New(Config{PollInterval: 9 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 10s")
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, client.pollInterval, "blank keeps the manifest default")
}

func TestNewCardTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[CardChangedEvent], 1))
	board := CardChangedTriggerConfiguration{BoardID: "AbCd1234"}
	for name, routes := range map[string][]CardChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target, Configuration: board}},
		"no target":  {{BindingName: "boardCards", Configuration: board}},
		"no board":   {{BindingName: "boardCards", Target: target}},
		"duplicate":  {{BindingName: "boardCards", Target: target, Configuration: board}, {BindingName: "boardCards", Target: target, Configuration: board}},
	} {
		_, err := NewCardTriggerRunner(CardTriggerRunnerConfig{Connection: connection, CardChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewCardTriggerRunner(CardTriggerRunnerConfig{CardChangedRoutes: []CardChangedTriggerRoute{{BindingName: "boardCards", Target: target, Configuration: board}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableCardTriggerRunnerRequiresASavedBoard(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	_, err := newDurableCardTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()}, ProjectCardTriggerRunnerConfig{
		CardChangedRoutes: []ProjectCardChangedTriggerRoute{{BindingName: "boardCards", Target: channelTarget(make(chan sdkgo.TriggerEvent[CardChangedEvent], 1))}},
	})
	require.ErrorContains(t, err, "save the watched board in Dex Web")
}

func TestDurableCardTriggerRunnerLoadsTheSavedBoardAndCommitsAfterRecording(t *testing.T) {
	board := trelloboardfake.New(t)
	board.CreateCard("Existing card")
	connection := newTriggerTestConnection(t, board.URL)
	saved, err := json.Marshal(CardChangedTriggerConfiguration{BoardID: board.BoardID})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "trello-cards", TriggerName: "cardChanged", BindingName: "boardCards", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[CardChangedEvent], 4)
	runner, err := newDurableCardTriggerRunner(connection, configuration, storage, ProjectCardTriggerRunnerConfig{
		CardChangedRoutes: []ProjectCardChangedTriggerRoute{{BindingName: "boardCards", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "trello-cards", TriggerName: "cardChanged", BindingName: "boardCards"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedToken := func() string {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil || record.State == nil {
			return ""
		}
		return record.State.Token
	}
	require.Eventually(t, func() bool { return committedToken() != "" }, 10*time.Second, 10*time.Millisecond)
	baseline := committedToken()
	created := board.CreateCard("Replace badge reader")
	runner.WakePoller("boardCards")

	select {
	case event := <-events:
		require.Equal(t, created, event.Payload.Card.ID, "the baseline card is never delivered")
		require.Equal(t, CardActionCreated, event.Payload.ActionType)
	case <-time.After(10 * time.Second):
		t.Fatal("the new card was not delivered")
	}
	require.Eventually(t, func() bool { return committedToken() != baseline }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedCardChangedTriggerBaselinesAndDeliversTheNextAction(t *testing.T) {
	board := trelloboardfake.New(t)
	existing := board.CreateCard("Existing card")
	connection := newTriggerTestConnection(t, board.URL)
	events := make(chan sdkgo.TriggerEvent[CardChangedEvent], 4)
	trigger := NewCardChangedTrigger(CardChangedTriggerConfig{
		Connection: connection, ConnectionName: "trello-cards", BindingName: "boardCards",
		Configuration: CardChangedTriggerConfiguration{BoardID: board.BoardID}, Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return board.ActionRequests() > 0 }, 10*time.Second, 10*time.Millisecond)
	board.CommentOnCard(existing, "Still open.")

	select {
	case event := <-events:
		require.Equal(t, CardActionCommented, event.Payload.ActionType)
		require.Equal(t, "Still open.", event.Payload.CommentText)
	case <-time.After(30 * time.Second):
		t.Fatal("the generated Trigger did not deliver the action after one poll interval")
	}
}
