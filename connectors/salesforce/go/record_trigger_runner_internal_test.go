// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/internal/salesforcepollfake"
	"github.com/superdurable/dex-connectors-library/connectors/salesforce/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "salesforce-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newTriggerTestConnection(t *testing.T, instanceURL string) Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "salesforce", Name: "salesforce-crm"}
	client, err := New(Config{PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		reference: {AuthMethodID: ProductionOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(pollTestToken), InstanceURL: instanceURL},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[RecordModifiedEvent]) sdkgo.TriggerTarget[RecordModifiedEvent] {
	return sdkgo.TriggerTargetFunc[RecordModifiedEvent](func(_ context.Context, event sdkgo.TriggerEvent[RecordModifiedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewRecordTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[RecordModifiedEvent], 1))
	opportunities := RecordModifiedTriggerConfiguration{SObjectType: "Opportunity"}
	for name, routes := range map[string][]RecordModifiedPolledTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target, Configuration: opportunities}},
		"no target":  {{BindingName: "opportunityChanges", Configuration: opportunities}},
		"no object":  {{BindingName: "opportunityChanges", Target: target}},
		"duplicate":  {{BindingName: "opportunityChanges", Target: target, Configuration: opportunities}, {BindingName: "opportunityChanges", Target: target, Configuration: opportunities}},
	} {
		_, err := NewRecordTriggerRunner(RecordTriggerRunnerConfig{Connection: connection, RecordModifiedPolledRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewRecordTriggerRunner(RecordTriggerRunnerConfig{RecordModifiedPolledRoutes: []RecordModifiedPolledTriggerRoute{{BindingName: "opportunityChanges", Target: target, Configuration: opportunities}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableRecordTriggerRunnerRequiresASavedObject(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	_, err := newDurableRecordTriggerRunner(connection, projectconfig.Configuration{}, storage, ProjectRecordTriggerRunnerConfig{
		RecordModifiedPolledRoutes: []ProjectRecordModifiedPolledTriggerRoute{{BindingName: "opportunityChanges", Target: channelTarget(nil)}},
	})
	require.ErrorContains(t, err, "has no saved object")
}

func TestDurableRecordTriggerRunnerLoadsTheSavedObjectAndCommitsAfterRecording(t *testing.T) {
	org := salesforcepollfake.New(t, pollTestToken, nil)
	existing := org.Insert("Opportunity", map[string]any{"Name": "Renewal"})
	connection := newTriggerTestConnection(t, org.URL)
	saved, err := json.Marshal(RecordModifiedTriggerConfiguration{SObjectType: "Opportunity", FieldNames: []string{"Name"}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "salesforce-crm", TriggerName: "recordModifiedPolled", BindingName: "opportunityChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[RecordModifiedEvent], 4)
	runner, err := newDurableRecordTriggerRunner(connection, configuration, storage, ProjectRecordTriggerRunnerConfig{
		RecordModifiedPolledRoutes: []ProjectRecordModifiedPolledTriggerRoute{{BindingName: "opportunityChanges", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "salesforce-crm", TriggerName: "recordModifiedPolled", BindingName: "opportunityChanges"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committedRecordID := func() string {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil || record.State == nil {
			return ""
		}
		var position recordPollCursor
		if json.Unmarshal([]byte(record.State.Token), &position) != nil {
			return ""
		}
		return position.RecordID
	}
	require.Eventually(t, func() bool { return committedRecordID() == existing }, 10*time.Second, 10*time.Millisecond)
	created := org.Insert("Opportunity", map[string]any{"Name": "Expansion"})
	runner.WakePoller("opportunityChanges")

	select {
	case event := <-events:
		require.Equal(t, created, event.Payload.RecordID, "the baseline record is never delivered")
		require.Equal(t, RecordChangeCreated, event.Payload.ChangeKind)
		name, _ := event.Payload.Record.StringField("Name")
		require.Equal(t, "Expansion", name)
	case <-time.After(10 * time.Second):
		t.Fatal("the new record was not delivered")
	}
	require.Eventually(t, func() bool { return committedRecordID() == created }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedRecordModifiedPolledTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	org := salesforcepollfake.New(t, pollTestToken, nil)
	existing := org.Insert("Case", map[string]any{})
	connection := newTriggerTestConnection(t, org.URL)
	events := make(chan sdkgo.TriggerEvent[RecordModifiedEvent], 4)
	trigger := NewRecordModifiedPolledTrigger(RecordModifiedPolledTriggerConfig{
		Connection: connection, ConnectionName: "salesforce-crm", BindingName: "caseChanges",
		Configuration: RecordModifiedTriggerConfiguration{SObjectType: "Case"}, Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return org.QueryCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	org.Update(existing, map[string]any{"Status": "Escalated"})

	select {
	case event := <-events:
		require.Equal(t, existing, event.Payload.RecordID)
		require.Equal(t, RecordChangeUpdated, event.Payload.ChangeKind)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}

func TestGeneratedRecordModifiedPolledTriggerRejectsAMissingObject(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	require.Panics(t, func() {
		NewRecordModifiedPolledTrigger(RecordModifiedPolledTriggerConfig{
			Connection: connection, ConnectionName: "salesforce-crm", BindingName: "caseChanges", Target: channelTarget(nil),
		})
	})
}
