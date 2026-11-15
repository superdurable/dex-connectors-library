// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/internal/hubspotsearchfake"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "hubspot-trigger-test", Kind: "live"}

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
	reference := sdkgo.ConnectionRef{Provider: "hubspot", Name: "hubspot-crm-watcher"}
	client, err := New(Config{Endpoint: endpoint, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString(hubspotsearchfake.AccessToken)},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[CRMObjectChangedEvent]) sdkgo.TriggerTarget[CRMObjectChangedEvent] {
	return sdkgo.TriggerTargetFunc[CRMObjectChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[CRMObjectChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewRejectsAPollIntervalBelowFiveSeconds(t *testing.T) {
	_, err := New(Config{PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
	client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, client.pollSchedule().Interval)
}

func TestNewCRMObjectTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[CRMObjectChangedEvent], 1))
	deals := CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeDeals}
	for name, routes := range map[string][]CRMObjectChangedTriggerRoute{
		"none":           nil,
		"no binding":     {{Target: target, Configuration: deals}},
		"no target":      {{BindingName: "dealChanges", Configuration: deals}},
		"no object type": {{BindingName: "dealChanges", Target: target}},
		"duplicate":      {{BindingName: "dealChanges", Target: target, Configuration: deals}, {BindingName: "dealChanges", Target: target, Configuration: deals}},
	} {
		_, err := NewCRMObjectTriggerRunner(CRMObjectTriggerRunnerConfig{Connection: connection, CRMObjectChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewCRMObjectTriggerRunner(CRMObjectTriggerRunnerConfig{CRMObjectChangedRoutes: []CRMObjectChangedTriggerRoute{{BindingName: "dealChanges", Target: target, Configuration: deals}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableRunnerRequiresASavedObjectType(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	_, err := newDurableCRMObjectTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()}, ProjectCRMObjectTriggerRunnerConfig{
		CRMObjectChangedRoutes: []ProjectCRMObjectChangedTriggerRoute{{BindingName: "dealChanges", Target: channelTarget(nil)}},
	})
	require.ErrorContains(t, err, "no saved object type")
}

func TestDurableRunnerLoadsTheSavedObjectTypeAndCommitsAfterRecording(t *testing.T) {
	fake := hubspotsearchfake.New(t, "deals", fakeStart)
	fake.Create(map[string]string{"dealname": "Existing renewal"})
	connection := newTriggerTestConnection(t, fake.URL)
	saved, err := json.Marshal(CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeDeals})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "hubspot-crm-watcher", TriggerName: "crmObjectChanged", BindingName: "dealChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[CRMObjectChangedEvent], 4)
	runner, err := newDurableCRMObjectTriggerRunner(connection, configuration, storage, ProjectCRMObjectTriggerRunnerConfig{
		CRMObjectChangedRoutes: []ProjectCRMObjectChangedTriggerRoute{{BindingName: "dealChanges", Properties: []string{"dealname"}, Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "hubspot-crm-watcher", TriggerName: "crmObjectChanged", BindingName: "dealChanges"}
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
	created := fake.Create(map[string]string{"dealname": "New expansion"})
	runner.WakePoller("dealChanges")

	select {
	case event := <-events:
		require.Equal(t, created, event.Payload.Object.ID, "the baseline record is never delivered")
		require.Equal(t, CRMObjectChangeCreated, event.Payload.ChangeKind)
		require.Equal(t, "New expansion", event.Payload.Object.Properties["dealname"])
	case <-time.After(10 * time.Second):
		t.Fatal("the new deal was not delivered")
	}
	require.Eventually(t, func() bool { return committedToken() != baseline }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedCRMObjectChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := hubspotsearchfake.New(t, "contacts", fakeStart)
	existing := fake.Create(map[string]string{"email": "ada@example.com"})
	connection := newTriggerTestConnection(t, fake.URL)
	events := make(chan sdkgo.TriggerEvent[CRMObjectChangedEvent], 4)
	trigger := NewCRMObjectChangedTrigger(CRMObjectChangedTriggerConfig{
		Connection: connection, ConnectionName: "hubspot-crm-watcher", BindingName: "contactChanges",
		Configuration: CRMObjectChangedTriggerConfiguration{ObjectType: ObjectTypeContacts, Properties: []string{"email"}}, Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return fake.SearchRequestCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Update(existing, map[string]string{"email": "ada.lovelace@example.com"})

	select {
	case event := <-events:
		require.Equal(t, CRMObjectChangeUpdated, event.Payload.ChangeKind)
		require.Equal(t, "ada.lovelace@example.com", event.Payload.Object.Properties["email"])
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
