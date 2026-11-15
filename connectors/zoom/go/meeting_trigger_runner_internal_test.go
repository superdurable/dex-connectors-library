// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package zoom

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/connectors/zoom/go/internal/zoommeetingsfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "zoom-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func channelTarget(events chan sdkgo.TriggerEvent[MeetingChangedEvent]) sdkgo.TriggerTarget[MeetingChangedEvent] {
	return sdkgo.TriggerTargetFunc[MeetingChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[MeetingChangedEvent]) error {
		events <- event
		return nil
	})
}

func receiveMeetingChange(t *testing.T, events chan sdkgo.TriggerEvent[MeetingChangedEvent]) sdkgo.TriggerEvent[MeetingChangedEvent] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("no meeting change was delivered")
		return sdkgo.TriggerEvent[MeetingChangedEvent]{}
	}
}

func TestNewMeetingTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1")
	target := channelTarget(make(chan sdkgo.TriggerEvent[MeetingChangedEvent], 1))
	for name, routes := range map[string][]MeetingChangedTriggerRoute{
		"none":         nil,
		"no binding":   {{Target: target}},
		"no target":    {{BindingName: "meetingChanges"}},
		"duplicate":    {{BindingName: "meetingChanges", Target: target}, {BindingName: "meetingChanges", Target: target}},
		"unknown kind": {{BindingName: "meetingChanges", Target: target, Configuration: MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{"ended"}}}},
	} {
		_, err := NewMeetingTriggerRunner(MeetingTriggerRunnerConfig{Connection: connection, MeetingChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewMeetingTriggerRunner(MeetingTriggerRunnerConfig{MeetingChangedRoutes: []MeetingChangedTriggerRoute{{BindingName: "meetingChanges", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableMeetingTriggerRunnerLoadsTheSavedKindsAndCommitsAfterRecording(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fake.Schedule(92674392836, "Consultation", 1)
	connection := newTriggerTestConnection(t, fake.URL)
	saved, err := json.Marshal(MeetingChangedTriggerConfiguration{ChangeKinds: []MeetingChangeKind{MeetingChangeScheduled, MeetingChangeUpdated}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: "zoom-watcher", TriggerName: "meetingChanged", BindingName: "hostMeetings", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[MeetingChangedEvent], 8)
	runner, err := newDurableMeetingTriggerRunner(connection, configuration, storage, ProjectMeetingTriggerRunnerConfig{
		MeetingChangedRoutes: []ProjectMeetingChangedTriggerRoute{{BindingName: "hostMeetings", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: "zoom-watcher", TriggerName: "meetingChanged", BindingName: "hostMeetings"}
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
	require.JSONEq(t, `{"version":1,"hostId":"zoom-host-1"}`, committed().Token)

	// A rename, a rename back, and the same rename again are three changes with three event IDs.
	topics := []string{"Consultation moved", "Consultation", "Consultation moved"}
	seen := map[string]bool{}
	for index, topic := range topics {
		fake.Rename(92674392836, topic)
		runner.WakePoller("hostMeetings")
		event := receiveMeetingChange(t, events)
		require.Equal(t, MeetingChangeUpdated, event.Payload.ChangeKind)
		require.Equal(t, topic, event.Payload.Meeting.Topic)
		require.False(t, seen[event.ID], "change %d reuses an earlier event ID", index)
		seen[event.ID] = true
		require.Eventually(t, func() bool { return committed().CommitCount == int64(index+2) }, 10*time.Second, 10*time.Millisecond)
	}

	fake.Delete(92674392836)
	fake.Schedule(92674392850, "Kickoff", 4)
	runner.WakePoller("hostMeetings")
	event := receiveMeetingChange(t, events)
	require.Equal(t, MeetingChangeScheduled, event.Payload.ChangeKind, "the saved kinds skip the removal")
	require.Equal(t, int64(92674392850), event.Payload.Meeting.ID)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedMeetingChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := zoommeetingsfake.New(t, triggerTestHostID)
	fake.Schedule(92674392836, "Consultation", 1)
	connection := newTriggerTestConnection(t, fake.URL)
	// A short interval keeps the test fast; New rejects one under the 30s minimum.
	connection.client.pollInterval = time.Second
	events := make(chan sdkgo.TriggerEvent[MeetingChangedEvent], 4)
	trigger := NewMeetingChangedTrigger(MeetingChangedTriggerConfig{
		Connection: connection, ConnectionName: "zoom-watcher", BindingName: "hostMeetings", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return fake.ListingRequestCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Delete(92674392836)

	event := receiveMeetingChange(t, events)
	require.Equal(t, MeetingChangeRemoved, event.Payload.ChangeKind)
	require.Equal(t, "Consultation", event.Payload.Meeting.Topic)
}
