// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package asana

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go/internal/asanaeventsfake"
	"github.com/superdurable/dex-connectors-library/connectors/asana/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "asana-trigger-test", Kind: "live"}

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
	client, reference := newFetcherTestClient(t, endpoint)
	connection, err := NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[TaskChangedEvent]) sdkgo.TriggerTarget[TaskChangedEvent] {
	return sdkgo.TriggerTargetFunc[TaskChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[TaskChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewTaskTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[TaskChangedEvent], 1))
	project := TaskChangedTriggerConfiguration{ProjectID: fetcherProjectID}
	for name, routes := range map[string][]TaskChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target, Configuration: project}},
		"no target":  {{BindingName: "projectTasks", Configuration: project}},
		"no project": {{BindingName: "projectTasks", Target: target}},
		"duplicate":  {{BindingName: "projectTasks", Target: target, Configuration: project}, {BindingName: "projectTasks", Target: target, Configuration: project}},
	} {
		_, err := NewTaskTriggerRunner(TaskTriggerRunnerConfig{Connection: connection, TaskChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewTaskTriggerRunner(TaskTriggerRunnerConfig{TaskChangedRoutes: []TaskChangedTriggerRoute{{BindingName: "projectTasks", Target: target, Configuration: project}}})
	require.Error(t, err, "a zero Connection is rejected")
	_, err = New(Config{PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "pollInterval must be at least 5s")
}

func TestDurableTaskTriggerRunnerRequiresASavedProject(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	_, err := newDurableTaskTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()}, ProjectTaskTriggerRunnerConfig{
		TaskChangedRoutes: []ProjectTaskChangedTriggerRoute{{BindingName: "projectTasks", Target: channelTarget(make(chan sdkgo.TriggerEvent[TaskChangedEvent], 1))}},
	})
	require.ErrorContains(t, err, "has no saved project")
}

func TestDurableTaskTriggerRunnerLoadsTheSavedProjectAndCommitsAfterRecording(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fake.AddTask("1204000000000001", "Existing")
	connection := newTriggerTestConnection(t, fake.URL)
	saved, err := json.Marshal(TaskChangedTriggerConfiguration{WorkspaceID: "1200000000000001", ProjectID: fetcherProjectID, ProjectName: "Facilities"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "asana-events", TriggerName: "taskChanged", BindingName: "projectTasks", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[TaskChangedEvent], 4)
	runner, err := newDurableTaskTriggerRunner(connection, configuration, storage, ProjectTaskTriggerRunnerConfig{
		TaskChangedRoutes: []ProjectTaskChangedTriggerRoute{{BindingName: "projectTasks", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "asana-events", TriggerName: "taskChanged", BindingName: "projectTasks"}
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
	require.JSONEq(t, `{"projectId":"1201000000000001","sync":"sync-1"}`, committedToken())
	fake.AddTask("1204000000000002", "Replace badge reader")
	runner.WakePoller("projectTasks")

	select {
	case event := <-events:
		require.Equal(t, "1204000000000002", event.Payload.Task.ID, "the baseline task is never delivered")
		require.Equal(t, TaskChangeAdded, event.Payload.Action)
	case <-time.After(10 * time.Second):
		t.Fatal("the new task was not delivered")
	}
	require.Eventually(t, func() bool {
		return committedToken() == `{"projectId":"1201000000000001","sync":"sync-2"}`
	}, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedTaskChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := asanaeventsfake.New(t, fetcherProjectID)
	fake.AddTask("1204000000000001", "Existing")
	connection := newTriggerTestConnection(t, fake.URL)
	events := make(chan sdkgo.TriggerEvent[TaskChangedEvent], 4)
	trigger := NewTaskChangedTrigger(TaskChangedTriggerConfig{
		Connection: connection, ConnectionName: "asana-events", BindingName: "projectTasks",
		Configuration: TaskChangedTriggerConfiguration{ProjectID: fetcherProjectID}, Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(fake.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.CompleteTask("1204000000000001")

	select {
	case event := <-events:
		require.Equal(t, TaskChangeChanged, event.Payload.Action)
		require.Equal(t, "completed", event.Payload.ChangedField)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
