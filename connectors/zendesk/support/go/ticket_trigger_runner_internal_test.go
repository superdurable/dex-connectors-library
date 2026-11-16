// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go/internal/zendeskexportfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "zendesk-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, apiBaseURL string) Connection {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[Credentials]{exportTestConnection: {
		Email: zendeskexportfake.Email, APIToken: sdkgo.NewSecretString(zendeskexportfake.APIToken),
	}}
	client, err := New(Config{Subdomain: "acme", PollInterval: minimumPollInterval}, credentials,
		WithAPIBaseURL(apiBaseURL), WithIncrementalExportRequestsPerMinute(600))
	require.NoError(t, err)
	connection, err := NewConnection(client, exportTestConnection)
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
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/api/v2")
	target := channelTarget(make(chan sdkgo.TriggerEvent[TicketChangedEvent], 1))
	for name, routes := range map[string][]TicketChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target}},
		"no target":  {{BindingName: "ticketChanges"}},
		"duplicate":  {{BindingName: "ticketChanges", Target: target}, {BindingName: "ticketChanges", Target: target}},
	} {
		_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{Connection: connection, TicketChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewTicketTriggerRunner(TicketTriggerRunnerConfig{TicketChangedRoutes: []TicketChangedTriggerRoute{{BindingName: "ticketChanges", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestNewRejectsAPollIntervalOrExportRateOutOfRange(t *testing.T) {
	credentials := sdkgo.StaticCredentialProvider[Credentials]{}
	_, err := New(Config{Subdomain: "acme", PollInterval: time.Second}, credentials)
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
	for _, requestsPerMinute := range []int{0, 601} {
		_, err = New(Config{Subdomain: "acme"}, credentials, WithIncrementalExportRequestsPerMinute(requestsPerMinute))
		require.ErrorContains(t, err, "1 to 600")
	}
}

func TestDurableTicketTriggerRunnerLoadsTheSavedConfigurationAndCommitsAfterRecording(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fake.Create(101, "Existing")
	connection := newTriggerTestConnection(t, fake.APIBaseURL())
	saved, err := json.Marshal(TicketChangedTriggerConfiguration{IncludeDeleted: true})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: exportTestConnection.Name, TriggerName: "ticketChanged", BindingName: "ticketChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	runner, err := newDurableTicketTriggerRunner(connection, configuration, storage, ProjectTicketTriggerRunnerConfig{
		TicketChangedRoutes: []ProjectTicketChangedTriggerRoute{{BindingName: "ticketChanges", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: exportTestConnection.Name, TriggerName: "ticketChanged", BindingName: "ticketChanges"}
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
	require.Eventually(t, func() bool { return strings.Contains(committedToken(), fake.Cursor()) }, 10*time.Second, 10*time.Millisecond)
	fake.Delete(101)
	runner.WakePoller("ticketChanges")

	select {
	case event := <-events:
		require.Equal(t, int64(101), event.Payload.Ticket.ID)
		require.Equal(t, TicketChangeDeleted, event.Payload.ChangeKind, "the saved includeDeleted reaches the poller")
	case <-time.After(10 * time.Second):
		t.Fatal("the deletion was not delivered")
	}
	require.Eventually(t, func() bool { return strings.Contains(committedToken(), fake.Cursor()) }, 10*time.Second, 10*time.Millisecond)
	require.NotContains(t, committedToken(), zendeskexportfake.APIToken)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedTicketChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := zendeskexportfake.New(t)
	fake.Create(101, "Existing")
	connection := newTriggerTestConnection(t, fake.APIBaseURL())
	events := make(chan sdkgo.TriggerEvent[TicketChangedEvent], 4)
	trigger := NewTicketChangedTrigger(TicketChangedTriggerConfig{
		Connection: connection, ConnectionName: exportTestConnection.Name, BindingName: "ticketChanges", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Update(101, "open")

	select {
	case event := <-events:
		require.Equal(t, TicketChangeUpdated, event.Payload.ChangeKind)
		require.Equal(t, TicketStatusOpen, event.Payload.Ticket.Status)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
