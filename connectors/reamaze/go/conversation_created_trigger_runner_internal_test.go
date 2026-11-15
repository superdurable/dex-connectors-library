// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go/internal/reamazefake"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "reamaze-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, baseURL string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "reamaze", Name: "reamaze-poll"}
	client, err := New(Config{Brand: "acme", PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {Email: pollTestEmail, APIToken: sdkgo.NewSecretString(pollTestToken)},
	}, WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[ConversationCreatedEvent]) sdkgo.TriggerTarget[ConversationCreatedEvent] {
	return sdkgo.TriggerTargetFunc[ConversationCreatedEvent](func(_ context.Context, event sdkgo.TriggerEvent[ConversationCreatedEvent]) error {
		events <- event
		return nil
	})
}

func TestPollIntervalBelowTheMinimumIsRejected(t *testing.T) {
	_, err := New(Config{Brand: "acme", PollInterval: 9 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 10s")
	client, err := New(Config{Brand: "acme"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, time.Minute, client.pollSchedule().Interval, "blank keeps the manifest default")
}

func TestNewConversationTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/api/v1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[ConversationCreatedEvent], 1))
	for name, routes := range map[string][]ConversationCreatedTriggerRoute{
		"none":        nil,
		"no binding":  {{Target: target}},
		"no target":   {{BindingName: "newConversations"}},
		"duplicate":   {{BindingName: "newConversations", Target: target}, {BindingName: "newConversations", Target: target}},
		"not a slug":  {{BindingName: "newConversations", Target: target, Configuration: ConversationCreatedTriggerConfiguration{Channel: "sales team"}}},
		"blank space": {{BindingName: " ", Target: target}},
	} {
		_, err := NewConversationTriggerRunner(ConversationTriggerRunnerConfig{Connection: connection, ConversationCreatedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewConversationTriggerRunner(ConversationTriggerRunnerConfig{ConversationCreatedRoutes: []ConversationCreatedTriggerRoute{{BindingName: "newConversations", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableConversationTriggerRunnerLoadsTheSavedChannelAndCommitsAfterRecording(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	provider.Insert(reamazefake.Conversation{Slug: "existing", Subject: "Existing", Channel: "support", CreatedAt: pollTestStart})
	connection := newTriggerTestConnection(t, provider.URL)
	saved, err := json.Marshal(ConversationCreatedTriggerConfiguration{Channel: "support"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "reamaze-poll", TriggerName: "conversationCreated", BindingName: "supportConversations", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[ConversationCreatedEvent], 4)
	runner, err := newDurableConversationTriggerRunner(connection, configuration, storage, ProjectConversationTriggerRunnerConfig{
		ConversationCreatedRoutes: []ProjectConversationCreatedTriggerRoute{{BindingName: "supportConversations", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "reamaze-poll", TriggerName: "conversationCreated", BindingName: "supportConversations"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedWatermark := func() int64 {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil || record.State == nil {
			return 0
		}
		var position conversationCreatedCursor
		require.NoError(t, json.Unmarshal([]byte(record.State.Token), &position))
		require.Equal(t, "support", position.Channel, "the saved channel is loaded")
		return position.WatermarkMillis
	}
	require.Eventually(t, func() bool { return committedWatermark() == pollTestStart.UnixMilli() }, 10*time.Second, 10*time.Millisecond)
	provider.Insert(reamazefake.Conversation{Slug: "refund", Subject: "Refund", Channel: "support", CreatedAt: pollTestStart.Add(time.Hour)})
	runner.WakePoller("supportConversations")

	select {
	case event := <-events:
		require.Equal(t, "refund", event.Payload.Conversation.ID, "the baseline conversation is never delivered")
	case <-time.After(10 * time.Second):
		t.Fatal("the new conversation was not delivered")
	}
	require.Eventually(t, func() bool { return committedWatermark() == pollTestStart.Add(time.Hour).UnixMilli() }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedConversationCreatedTriggerBaselinesAndDeliversTheNextConversation(t *testing.T) {
	provider := reamazefake.New(t, pollTestEmail, pollTestToken)
	provider.Insert(reamazefake.Conversation{Slug: "existing", Subject: "Existing", CreatedAt: pollTestStart})
	connection := newTriggerTestConnection(t, provider.URL)
	events := make(chan sdkgo.TriggerEvent[ConversationCreatedEvent], 4)
	trigger := NewConversationCreatedTrigger(ConversationCreatedTriggerConfig{
		Connection: connection, ConnectionName: "reamaze-poll", BindingName: "allConversations", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return provider.ListRequests() > 0 }, 10*time.Second, 10*time.Millisecond)
	provider.Insert(reamazefake.Conversation{Slug: "fresh", Subject: "Fresh", CreatedAt: pollTestStart.Add(time.Minute)})

	select {
	case event := <-events:
		require.Equal(t, "fresh", event.Payload.Conversation.ID)
		require.Equal(t, "acme", event.Payload.Brand)
	case <-time.After(30 * time.Second):
		t.Fatal("the generated Trigger did not deliver the conversation after one poll interval")
	}
}
