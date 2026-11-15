// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go/internal/twiliofake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "twilio-trigger-test", Kind: "live"}

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
	client, err := New(Config{AccountSID: fetcherAccountSID, Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherConnection: {AuthMethodID: AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(fetcherAuthToken)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, fetcherConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[MessageReceivedEvent]) sdkgo.TriggerTarget[MessageReceivedEvent] {
	return sdkgo.TriggerTargetFunc[MessageReceivedEvent](func(_ context.Context, event sdkgo.TriggerEvent[MessageReceivedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewMessageTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/2010-04-01")
	target := channelTarget(make(chan sdkgo.TriggerEvent[MessageReceivedEvent], 1))
	for name, routes := range map[string][]MessageReceivedTriggerRoute{
		"none":        nil,
		"no binding":  {{Target: target}},
		"no target":   {{BindingName: "inbound"}},
		"duplicate":   {{BindingName: "inbound", Target: target}, {BindingName: "inbound", Target: target}},
		"bad address": {{BindingName: "inbound", Target: target, Configuration: MessageReceivedTriggerConfiguration{To: "4155550100"}}},
	} {
		_, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{Connection: connection, MessageReceivedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewMessageTriggerRunner(MessageTriggerRunnerConfig{MessageReceivedRoutes: []MessageReceivedTriggerRoute{{BindingName: "inbound", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableMessageTriggerRunnerLoadsTheSavedAddressAndCommitsAfterRecording(t *testing.T) {
	provider := twiliofake.New(t, fetcherAccountSID, fetcherAccountSID, fetcherAuthToken)
	provider.Receive(fetcherCustomer, fetcherNumber, "existing", time.Now().Add(-10*time.Second), "")
	connection := newTriggerTestConnection(t, provider.URL)
	saved, err := json.Marshal(MessageReceivedTriggerConfiguration{To: fetcherNumber})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: fetcherConnection.Name, TriggerName: "messageReceived", BindingName: "support-line", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[MessageReceivedEvent], 4)
	runner, err := newDurableMessageTriggerRunner(connection, configuration, storage, ProjectMessageTriggerRunnerConfig{
		MessageReceivedRoutes: []ProjectMessageReceivedTriggerRoute{{BindingName: "support-line", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: fetcherConnection.Name, TriggerName: "messageReceived", BindingName: "support-line"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committed := func() *projectconfig.PollCursorState {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil {
			return nil
		}
		return record.State
	}
	require.Eventually(t, func() bool { return committed() != nil }, 10*time.Second, 10*time.Millisecond)
	require.Contains(t, committed().Token, `"to":"`+fetcherNumber+`"`, "the saved receiving address scopes the cursor")
	sid := provider.Receive(fetcherCustomer, fetcherNumber, "new", time.Now(), "")
	provider.Receive(fetcherCustomer, fetcherOtherNum, "elsewhere", time.Now(), "")
	runner.WakePoller("support-line")

	select {
	case event := <-events:
		require.Equal(t, "twilio-message-"+sid, event.ID, "the baseline message is never delivered")
		require.Equal(t, "new", event.Payload.Body)
	case <-time.After(10 * time.Second):
		t.Fatal("the new message was not delivered")
	}
	require.Eventually(t, func() bool {
		state := committed()
		return state != nil && state.CommitCount >= 2 && len(state.Snapshot.Entries) == 2
	}, 10*time.Second, 10*time.Millisecond, "the snapshot keeps the baseline and delivered SIDs within the lookback")
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
	require.Empty(t, events, "a message to another number is not delivered")
}

func TestGeneratedMessageReceivedTriggerBaselinesAndDeliversTheNextMessage(t *testing.T) {
	provider := twiliofake.New(t, fetcherAccountSID, fetcherAccountSID, fetcherAuthToken)
	provider.Receive(fetcherCustomer, fetcherNumber, "existing", time.Now().Add(-10*time.Second), "")
	connection := newTriggerTestConnection(t, provider.URL)
	events := make(chan sdkgo.TriggerEvent[MessageReceivedEvent], 4)
	trigger := NewMessageReceivedTrigger(MessageReceivedTriggerConfig{
		Connection: connection, ConnectionName: fetcherConnection.Name, BindingName: "every-inbound", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(provider.ListRequests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	sid := provider.Receive(fetcherCustomer, fetcherOtherNum, "hi", time.Now(), "")

	select {
	case event := <-events:
		require.Equal(t, "twilio-message-"+sid, event.ID)
		require.Equal(t, fetcherOtherNum, event.Payload.Message.To, "a blank address receives every number")
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the message after one poll interval")
	}
}
