// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/go/internal/calendarsyncfake"
	"github.com/superdurable/dex-connectors-library/connectors/google/calendar/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "calendar-trigger-test", Kind: "live"}

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
	reference := sdkgo.ConnectionRef{Provider: "google", Name: "calendar-sync"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: GoogleOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString("calendar-token")},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[EventChangedEvent]) sdkgo.TriggerTarget[EventChangedEvent] {
	return sdkgo.TriggerTargetFunc[EventChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[EventChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewEventTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[EventChangedEvent], 1))
	for name, routes := range map[string][]EventChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target}},
		"no target":  {{BindingName: "calendarChanges"}},
		"duplicate":  {{BindingName: "calendarChanges", Target: target}, {BindingName: "calendarChanges", Target: target}},
		"whitespace": {{BindingName: "calendarChanges", Target: target, Configuration: EventChangedTriggerConfiguration{CalendarID: "primary "}}},
	} {
		_, err := NewEventTriggerRunner(EventTriggerRunnerConfig{Connection: connection, EventChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewEventTriggerRunner(EventTriggerRunnerConfig{EventChangedRoutes: []EventChangedTriggerRoute{{BindingName: "calendarChanges", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableEventTriggerRunnerLoadsTheSavedCalendarAndCommitsAfterRecording(t *testing.T) {
	fake := calendarsyncfake.New(t, syncCalendarID)
	fake.Insert("existing001", "Existing")
	connection := newTriggerTestConnection(t, fake.URL)
	saved, err := json.Marshal(EventChangedTriggerConfiguration{CalendarID: syncCalendarID, CalendarName: "Ops Team"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "calendar-sync", TriggerName: "eventChanged", BindingName: "teamChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[EventChangedEvent], 4)
	runner, err := newDurableEventTriggerRunner(connection, configuration, storage, ProjectEventTriggerRunnerConfig{
		EventChangedRoutes: []ProjectEventChangedTriggerRoute{{BindingName: "teamChanges", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "calendar-sync", TriggerName: "eventChanged", BindingName: "teamChanges"}
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
	require.JSONEq(t, `{"calendarId":"team@group.calendar.google.com","syncToken":"sync-1"}`, committedToken())
	fake.Insert("kickoff0001", "Kickoff")
	runner.WakePoller("teamChanges")

	select {
	case event := <-events:
		require.Equal(t, "kickoff0001", event.Payload.Event.ID, "the baseline event is never delivered")
		require.Equal(t, EventChangeCreated, event.Payload.ChangeKind)
	case <-time.After(10 * time.Second):
		t.Fatal("the new event was not delivered")
	}
	require.Eventually(t, func() bool {
		return committedToken() == `{"calendarId":"team@group.calendar.google.com","syncToken":"sync-2"}`
	}, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedEventChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := calendarsyncfake.New(t, "primary")
	fake.Insert("existing001", "Existing")
	connection := newTriggerTestConnection(t, fake.URL)
	events := make(chan sdkgo.TriggerEvent[EventChangedEvent], 4)
	trigger := NewEventChangedTrigger(EventChangedTriggerConfig{
		Connection: connection, ConnectionName: "calendar-sync", BindingName: "primaryChanges", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Cancel("existing001")

	select {
	case event := <-events:
		require.Equal(t, EventChangeCancelled, event.Payload.ChangeKind)
		require.Equal(t, "primary", event.Payload.CalendarID)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
