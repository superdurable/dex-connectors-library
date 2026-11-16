// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hiver

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go/internal/hiverfake"
	"github.com/superdurable/dex-connectors-library/connectors/hiver/go/internal/projectstoretest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/pollingtrigger"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// recordingConversationTarget collects delivered conversation IDs.
type recordingConversationTarget struct {
	mutex    sync.Mutex
	eventIDs []string
}

func (target *recordingConversationTarget) HandleTrigger(_ context.Context, event sdkgo.TriggerEvent[ConversationReceivedEvent]) error {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	target.eventIDs = append(target.eventIDs, event.ID)
	return nil
}

func (target *recordingConversationTarget) delivered() []string {
	target.mutex.Lock()
	defer target.mutex.Unlock()
	return slices.Clone(target.eventIDs)
}

func newConversationReceivedTestConnection(t *testing.T, baseURL string) Connection {
	t.Helper()
	connection, err := NewConnection(newConversationReceivedTestClient(t, baseURL, Config{PollInterval: time.Second}), testConnectionReference)
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

func TestGeneratedConversationReceivedTriggerRecordsABaselineThenDeliversNewConversations(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, "573741001")
	connection := newConversationReceivedTestConnection(t, server.URL)
	target := &recordingConversationTarget{}
	trigger := NewConversationReceivedTrigger(ConversationReceivedTriggerConfig{
		Connection: connection, ConnectionName: "hiver-support", BindingName: "new-conversations",
		Configuration: ConversationReceivedTriggerConfiguration{InboxID: testInboxID}, Target: target,
	})
	runUntil(t, trigger.Run, func() bool {
		if server.ConversationListRequests() == 1 {
			server.AddConversations(testInboxID, "573741002")
		}
		return len(target.delivered()) == 1
	})
	require.Equal(t, []string{"conversation-105902-573741002"}, target.delivered())

	invalid := NewConversationReceivedTrigger(ConversationReceivedTriggerConfig{
		Connection: connection, ConnectionName: "hiver-support", BindingName: "new-conversations", Target: target,
	})
	require.ErrorContains(t, invalid.Run(context.Background()), "inboxId")
}

func TestConversationReceivedTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newConversationReceivedTestConnection(t, "https://api2.hiverhq.com/v1")
	valid := ConversationReceivedTriggerRoute{
		BindingName: "new-conversations", Configuration: ConversationReceivedTriggerConfiguration{InboxID: testInboxID},
		Cursor: pollingtrigger.NewMemoryCursorStore(), Target: &recordingConversationTarget{},
	}
	_, err := NewConversationReceivedTriggerRunner(ConversationReceivedTriggerRunnerConfig{Connection: connection, Routes: []ConversationReceivedTriggerRoute{valid}})
	require.NoError(t, err)
	for name, route := range map[string]ConversationReceivedTriggerRoute{
		"binding":       {Configuration: valid.Configuration, Cursor: valid.Cursor, Target: valid.Target},
		"cursor":        {BindingName: valid.BindingName, Configuration: valid.Configuration, Target: valid.Target},
		"target":        {BindingName: valid.BindingName, Configuration: valid.Configuration, Cursor: valid.Cursor},
		"configuration": {BindingName: valid.BindingName, Cursor: valid.Cursor, Target: valid.Target},
	} {
		_, err := NewConversationReceivedTriggerRunner(ConversationReceivedTriggerRunnerConfig{Connection: connection, Routes: []ConversationReceivedTriggerRoute{route}})
		require.Error(t, err, name)
	}
	duplicate := valid
	duplicate.Cursor = pollingtrigger.NewMemoryCursorStore()
	_, err = NewConversationReceivedTriggerRunner(ConversationReceivedTriggerRunnerConfig{Connection: connection, Routes: []ConversationReceivedTriggerRoute{valid, duplicate}})
	require.Error(t, err)
	_, err = NewConversationReceivedTriggerRunner(ConversationReceivedTriggerRunnerConfig{Connection: connection})
	require.Error(t, err)
	_, err = NewConversationReceivedTriggerRunner(ConversationReceivedTriggerRunnerConfig{Routes: []ConversationReceivedTriggerRoute{valid}})
	require.Error(t, err)
}

func TestDurableConversationReceivedTriggerRunnerKeepsItsCursorAcrossRestarts(t *testing.T) {
	server := hiverfake.New(testInboxID, "support@acme.example.com")
	defer server.Close()
	server.AddConversations(testInboxID, conversationIDs(573741000, 150)...)
	connection := newConversationReceivedTestConnection(t, server.URL)
	store := projectstoretest.New()
	bindingConfiguration, err := json.Marshal(ConversationReceivedTriggerConfiguration{InboxID: testInboxID})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "hiver-support", TriggerName: "conversationReceived",
		BindingName: "new-conversations", Configuration: bindingConfiguration,
	}}}
	openCursor := func(key projectconfig.PollCursorKey) (pollingtrigger.CursorStore, error) { return store.Cursor(key) }
	target := &recordingConversationTarget{}
	newRunner := func() *ConversationReceivedTriggerRunner {
		runner, err := newDurableConversationReceivedTriggerRunner(connection, configuration, store.Inbox, openCursor,
			[]ProjectConversationReceivedTriggerRoute{{BindingName: "new-conversations", Target: target}})
		require.NoError(t, err)
		return runner
	}
	runUntil(t, newRunner().Run, func() bool {
		if server.ConversationListRequests() == 2 {
			server.AddConversations(testInboxID, "573742001")
		}
		return len(target.delivered()) == 1
	})
	// Conversations that arrive while no runner polls are delivered after the restart; no new baseline runs.
	server.AddConversations(testInboxID, "573742002", "573742003")
	runUntil(t, newRunner().Run, func() bool { return len(target.delivered()) == 3 })
	require.Equal(t, []string{"conversation-105902-573742001", "conversation-105902-573742003", "conversation-105902-573742002"}, target.delivered())
	pending, err := store.PendingEventIDs(context.Background())
	require.NoError(t, err)
	require.Empty(t, pending)

	_, err = newDurableConversationReceivedTriggerRunner(connection, configuration, store.Inbox, openCursor,
		[]ProjectConversationReceivedTriggerRoute{{BindingName: "unsaved-binding", Target: target}})
	require.ErrorContains(t, err, "unsaved-binding")
}
