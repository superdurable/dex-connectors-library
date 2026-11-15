// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go/internal/bamboohrfake"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "bamboohr-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func newRunnerTestConnection(t *testing.T, apiBaseURL string) Connection {
	t.Helper()
	client, err := New(Config{CompanyDomain: "acme", PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {APIKey: sdkgo.NewSecretString(bamboohrfake.APIKey)},
	}, WithAPIBaseURL(apiBaseURL))
	require.NoError(t, err)
	connection, err := NewConnection(client, triggerTestConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[EmployeeChangedEvent]) sdkgo.TriggerTarget[EmployeeChangedEvent] {
	return sdkgo.TriggerTargetFunc[EmployeeChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[EmployeeChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewClientRejectsAShortPollInterval(t *testing.T) {
	_, err := New(Config{CompanyDomain: "acme", PollInterval: 9 * time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 10s")
	client, err := New(Config{CompanyDomain: "acme"}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	require.Equal(t, time.Minute, client.pollInterval, "a blank pollInterval keeps the manifest default")
}

func TestNewEmployeeChangedTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRunnerTestConnection(t, "http://127.0.0.1:1/api/v1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[EmployeeChangedEvent], 1))
	for name, routes := range map[string][]EmployeeChangedTriggerRoute{
		"none":        nil,
		"no binding":  {{Target: target}},
		"no target":   {{BindingName: "hires"}},
		"duplicate":   {{BindingName: "hires", Target: target}, {BindingName: "hires", Target: target}},
		"change type": {{BindingName: "hires", Target: target, Configuration: EmployeeChangedTriggerConfiguration{ChangeType: "all"}}},
	} {
		_, err := NewEmployeeChangedTriggerRunner(EmployeeChangedTriggerRunnerConfig{Connection: connection, Routes: routes})
		require.Error(t, err, name)
	}
	_, err := NewEmployeeChangedTriggerRunner(EmployeeChangedTriggerRunnerConfig{Routes: []EmployeeChangedTriggerRoute{{BindingName: "hires", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
	_, err = NewProjectEmployeeChangedTriggerRunner(nil, "bamboohr", nil)
	require.ErrorContains(t, err, "loaded project configuration")
}

func TestDurableRunnerLoadsTheSavedChangeTypeAndCommitsAfterRecording(t *testing.T) {
	fake := bamboohrfake.New(t)
	fake.Insert("140", nil)
	connection := newRunnerTestConnection(t, fake.APIBaseURL())
	saved, err := json.Marshal(EmployeeChangedTriggerConfiguration{ChangeType: EmployeeChangeTypeInserted})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "employeeChanged", BindingName: "hires", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[EmployeeChangedEvent], 4)
	runner, err := newDurableEmployeeChangedTriggerRunner(connection, configuration, storage,
		[]ProjectEmployeeChangedTriggerRoute{{BindingName: "hires", Target: channelTarget(events)}})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "employeeChanged", BindingName: "hires"}
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
	require.JSONEq(t, `{"v":1,"since":"2026-10-13T09:00:01Z","changeType":"inserted"}`, committedToken())
	fake.Update("140", nil)
	fake.Insert("141", nil)
	fake.Advance(employeeChangeSettleWindow)
	runner.WakePoller("hires")

	select {
	case event := <-events:
		require.Equal(t, "141", event.Payload.EmployeeID, "the seeded and updated employees are not delivered")
		require.Equal(t, EmployeeChangeActionInserted, event.Payload.Action)
	case <-time.After(10 * time.Second):
		t.Fatal("the new employee was not delivered")
	}
	require.Eventually(t, func() bool {
		return committedToken() == `{"v":1,"since":"2026-10-13T09:00:03Z","afterEmployeeId":"141","changeType":"inserted"}`
	}, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedEmployeeChangedTriggerSeedsAndDeliversTheNextChange(t *testing.T) {
	fake := bamboohrfake.New(t)
	fake.Insert("140", nil)
	connection := newRunnerTestConnection(t, fake.APIBaseURL())
	events := make(chan sdkgo.TriggerEvent[EmployeeChangedEvent], 4)
	trigger := NewEmployeeChangedTrigger(EmployeeChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerTestConnection.Name, BindingName: "changes", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.ChangeListQueries()) >= 1 }, 10*time.Second, 10*time.Millisecond)
	fake.Delete("140")
	fake.Advance(employeeChangeSettleWindow)

	select {
	case event := <-events:
		require.Equal(t, EmployeeChangedEvent{EmployeeID: "140", Action: EmployeeChangeActionDeleted, LastChanged: bamboohrfake.Start.Add(2 * time.Second)}, event.Payload)
	case <-time.After(30 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
	require.Panics(t, func() {
		NewEmployeeChangedTrigger(EmployeeChangedTriggerConfig{
			Connection: connection, ConnectionName: triggerTestConnection.Name, BindingName: "changes",
			Configuration: EmployeeChangedTriggerConfiguration{ChangeType: "Deleted"}, Target: channelTarget(events),
		})
	})
}
