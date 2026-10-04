// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package projectconfig_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var inboxScope = projectconfig.Scope{ProjectID: "inbox", Kind: "live"}

var inboxKey = projectconfig.TriggerInboxKey{ConnectorID: "gmail", ConnectionName: "support mail", TriggerName: "messageReceived", BindingName: "new-ticket"}

func pendingIDs(t *testing.T, inbox *projectconfig.TriggerInbox) []string {
	t.Helper()
	pending, err := inbox.Pending(context.Background())
	require.NoError(t, err)
	ids := []string{}
	for _, event := range pending {
		ids = append(ids, event.ID)
	}
	return ids
}

func event(id string) projectconfig.PendingTriggerEvent {
	return projectconfig.PendingTriggerEvent{ID: id, Event: json.RawMessage(`{"id":"` + id + `"}`)}
}

func TestTriggerInboxKeepsOrderAndAddsEachEventOnce(t *testing.T) {
	ctx := context.Background()
	inbox, err := projectconfig.NewTriggerInbox(testsupport.NewObjectStore(), inboxScope, inboxKey)
	require.NoError(t, err)
	require.Empty(t, pendingIDs(t, inbox))

	require.NoError(t, inbox.Add(ctx, event("a")))
	require.NoError(t, inbox.Add(ctx, event("b")))
	require.NoError(t, inbox.Add(ctx, event("a")))
	require.Equal(t, []string{"a", "b"}, pendingIDs(t, inbox))

	require.NoError(t, inbox.Remove(ctx, "a"))
	require.NoError(t, inbox.Remove(ctx, "a"))
	require.Equal(t, []string{"b"}, pendingIDs(t, inbox))
}

func TestTriggerInboxIsSharedByReplicasOfOneBinding(t *testing.T) {
	ctx := context.Background()
	objects := testsupport.NewObjectStore()
	first, err := projectconfig.NewTriggerInbox(objects, inboxScope, inboxKey)
	require.NoError(t, err)
	restarted, err := projectconfig.NewTriggerInbox(objects, inboxScope, inboxKey)
	require.NoError(t, err)
	otherBinding := inboxKey
	otherBinding.BindingName = "escalation"
	other, err := projectconfig.NewTriggerInbox(objects, inboxScope, otherBinding)
	require.NoError(t, err)

	require.NoError(t, first.Add(ctx, event("a")))
	require.Equal(t, []string{"a"}, pendingIDs(t, restarted))
	require.Empty(t, pendingIDs(t, other))
}

// racingStore lets another replica write the inbox once between a read and the conditional write.
type racingStore struct {
	*testsupport.ObjectStore
	race func()
}

func (store *racingStore) CompareAndSwapObject(ctx context.Context, key, expectedETag string, contents []byte) (projectconfig.Object, error) {
	if race := store.race; race != nil {
		store.race = nil
		race()
	}
	return store.ObjectStore.CompareAndSwapObject(ctx, key, expectedETag, contents)
}

func TestTriggerInboxRetriesAfterAConcurrentWrite(t *testing.T) {
	ctx := context.Background()
	objects := &racingStore{ObjectStore: testsupport.NewObjectStore()}
	inbox, err := projectconfig.NewTriggerInbox(objects, inboxScope, inboxKey)
	require.NoError(t, err)
	replica, err := projectconfig.NewTriggerInbox(objects.ObjectStore, inboxScope, inboxKey)
	require.NoError(t, err)
	require.NoError(t, inbox.Add(ctx, event("a")))

	objects.race = func() { require.NoError(t, replica.Add(ctx, event("b"))) }
	require.NoError(t, inbox.Add(ctx, event("c")))
	require.Equal(t, []string{"a", "b", "c"}, pendingIDs(t, inbox))
}

func TestTriggerInboxRejectsInvalidIdentity(t *testing.T) {
	objects := testsupport.NewObjectStore()
	for _, key := range []projectconfig.TriggerInboxKey{
		{ConnectorID: "Gmail", ConnectionName: "a", TriggerName: "messageReceived", BindingName: "b"},
		{ConnectorID: "gmail", ConnectionName: "", TriggerName: "messageReceived", BindingName: "b"},
		{ConnectorID: "gmail", ConnectionName: "a", TriggerName: "message-received", BindingName: "b"},
		{ConnectorID: "gmail", ConnectionName: "a", TriggerName: "messageReceived", BindingName: " b"},
	} {
		_, err := projectconfig.NewTriggerInbox(objects, inboxScope, key)
		require.Error(t, err, "%+v", key)
	}
}
