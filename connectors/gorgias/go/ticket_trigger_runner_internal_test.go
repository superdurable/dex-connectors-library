// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go/internal/gorgiaseventfake"
	"github.com/superdurable/dex-connectors-library/connectors/gorgias/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "gorgias-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newRunnerTestConnection(t *testing.T, baseURL string) Connection {
	t.Helper()
	connection, err := NewConnection(newTriggerTestClient(t, baseURL), triggerTestConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[TicketChangedEvent]) sdkgo.TriggerTarget[TicketChangedEvent] {
	return sdkgo.TriggerTargetFunc[TicketChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[TicketChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewTicketTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRunnerTestConnection(t, "http://127.0.0.1:1/api")
	target := channelTarget(make(chan sdkgo.TriggerEvent[TicketChangedEvent], 1))
	for name, routes := range map[string][]TicketChangedTriggerRoute{
		"none":         nil,
		"no binding":   {{Target: target}},
		"no target":    {{BindingName: "ticketEvents"}},
		"duplicate":    {{BindingName: "ticketEvents", Target: target}, {BindingName: "ticketEvents", Target: target}},
		"unknown type": {{BindingName: "ticketEvents", Target: target, Configuration: TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{"ticket-message-created"}}}},
	} {
		_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{TicketChangedRoutes: []TicketChangedTriggerRoute{{BindingName: "ticketEvents", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableTicketTriggerRunnerLoadsTheSavedTypesAndCommitsAfterRecording(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fake.RecordTicketEvent("ticket-created", 7001, 0)
	connection := newRunnerTestConnection(t, fake.BaseURL)
	saved, err := json.Marshal(TicketChangedTriggerConfiguration{EventTypes: []TicketEventType{TicketEventClosed}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "ticketChanged", BindingName: "closedTickets", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	runner, err := newDurableTicketTriggerRunner(connection, configuration, storage, ProjectTicketTriggerRunnerConfig{
		TicketChangedRoutes: []ProjectTicketChangedTriggerRoute{{BindingName: "closedTickets", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "ticketChanged", BindingName: "closedTickets"}
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
	fake.RecordTicketEvent("ticket-updated", 7001, 12)
	closed := fake.RecordTicketEvent("ticket-closed", 7001, 12)
	runner.WakePoller("closedTickets")

	select {
	case event := <-events:
		require.Equal(t, ticketEventKey(closed), event.ID, "the saved configuration delivers only ticket-closed")
		require.Equal(t, int64(7001), event.Payload.TicketID)
	case <-time.After(10 * time.Second):
		t.Fatal("the closed ticket was not delivered")
	}
	require.Eventually(t, func() bool { return committedToken() != baseline }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
	require.Empty(t, events, "the skipped update is never delivered")
}

func TestGeneratedTicketChangedTriggerBaselinesAndDeliversTheNextEvent(t *testing.T) {
	fake := gorgiaseventfake.New(t, triggerTestEmail, triggerTestAPIKey)
	fake.RecordTicketEvent("ticket-created", 7001, 0)
	client, err := New(Config{Domain: "acme", PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {Email: triggerTestEmail, APIKey: sdkgo.NewSecretString(triggerTestAPIKey)},
	}, WithAPIBaseURL(fake.BaseURL))
	require.NoError(t, err)
	connection, err := NewConnection(client, triggerTestConnection)
	require.NoError(t, err)
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	trigger := NewTicketChangedTrigger(TicketChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerTestConnection.Name, BindingName: "allTicketEvents", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return fake.EventRequestCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	reopened := fake.RecordTicketEvent("ticket-reopened", 7001, 12)

	select {
	case event := <-events:
		require.Equal(t, ticketEventKey(reopened), event.ID)
		require.Equal(t, TicketEventReopened, event.Payload.EventType)
	case <-time.After(30 * time.Second):
		t.Fatal("the generated Trigger did not deliver the event after one poll interval")
	}
}

func TestNewRejectsAShortPollInterval(t *testing.T) {
	_, err := New(Config{Domain: "acme", PollInterval: 5 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 10s")
	_, err = New(Config{Domain: "acme", PollInterval: -time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.Error(t, err)
}
