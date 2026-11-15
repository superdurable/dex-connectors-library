// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/internal/freshdeskticketfake"
	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "freshdesk-trigger-test", Kind: "live"}

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
	connection := newRunnerTestConnection(t, "http://127.0.0.1:1/api/v2")
	target := channelTarget(make(chan sdkgo.TriggerEvent[TicketChangedEvent], 1))
	for name, routes := range map[string][]TicketChangedTriggerRoute{
		"none":         nil,
		"no binding":   {{Target: target}},
		"no target":    {{BindingName: "ticketChanges"}},
		"duplicate":    {{BindingName: "ticketChanges", Target: target}, {BindingName: "ticketChanges", Target: target}},
		"unknown kind": {{BindingName: "ticketChanges", Target: target, Configuration: TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{"merged"}}}},
	} {
		_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{TicketChangedRoutes: []TicketChangedTriggerRoute{{BindingName: "ticketChanges", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableTicketTriggerRunnerLoadsTheSavedKindsAndCommitsAfterRecording(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(1001, "Existing", now.Add(-time.Hour), now.Add(-10*time.Minute))
	connection := newRunnerTestConnection(t, fake.BaseURL)
	saved, err := json.Marshal(TicketChangedTriggerConfiguration{ChangeKinds: []TicketChangeKind{TicketChangeCreated}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "ticketChanged", BindingName: "newTickets", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	runner, err := newDurableTicketTriggerRunner(connection, configuration, storage, ProjectTicketTriggerRunnerConfig{
		TicketChangedRoutes: []ProjectTicketChangedTriggerRoute{{BindingName: "newTickets", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "ticketChanged", BindingName: "newTickets"}
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
	fake.Update(1001, "Existing, edited", 3)
	fake.Create(1002, "Double charge")
	runner.WakePoller("newTickets")

	select {
	case event := <-events:
		require.Equal(t, int64(1002), event.Payload.Ticket.ID, "the saved configuration delivers only created tickets")
		require.Equal(t, TicketChangeCreated, event.Payload.ChangeKind)
	case <-time.After(10 * time.Second):
		t.Fatal("the new ticket was not delivered")
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

func TestGeneratedTicketChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := freshdeskticketfake.New(t, triggerTestAPIKey)
	now := time.Now().UTC().Truncate(time.Second)
	fake.CreateAt(1001, "Existing", now.Add(-time.Hour), now.Add(-10*time.Minute))
	client, err := New(Config{Domain: "acme", PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {APIKey: sdkgo.NewSecretString(triggerTestAPIKey)},
	}, WithAPIBaseURL(fake.BaseURL))
	require.NoError(t, err)
	connection, err := NewConnection(client, triggerTestConnection)
	require.NoError(t, err)
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	trigger := NewTicketChangedTrigger(TicketChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerTestConnection.Name, BindingName: "allTickets", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return fake.ListRequestCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Update(1001, "Existing, escalated", 3)

	select {
	case event := <-events:
		require.Equal(t, TicketChangeUpdated, event.Payload.ChangeKind)
		require.Equal(t, "Existing, escalated", event.Payload.Ticket.Subject)
	case <-time.After(30 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
