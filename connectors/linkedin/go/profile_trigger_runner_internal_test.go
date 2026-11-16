// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linkedin

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/linkedinprofilefake"
	"github.com/superdurable/dex-connectors-library/connectors/linkedin/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "linkedin-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

func channelTarget(events chan sdkgo.TriggerEvent[ProfileChangedEvent]) sdkgo.TriggerTarget[ProfileChangedEvent] {
	return sdkgo.TriggerTargetFunc[ProfileChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[ProfileChangedEvent]) error {
		events <- event
		return nil
	})
}

func receiveProfileChange(t *testing.T, events chan sdkgo.TriggerEvent[ProfileChangedEvent]) sdkgo.TriggerEvent[ProfileChangedEvent] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("no profile change was delivered")
		return sdkgo.TriggerEvent[ProfileChangedEvent]{}
	}
}

func TestNewProfileTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/v2/userinfo", linkedinprofilefake.AccessToken)
	target := channelTarget(make(chan sdkgo.TriggerEvent[ProfileChangedEvent], 1))
	for name, routes := range map[string][]ProfileChangedTriggerRoute{
		"none":       nil,
		"no binding": {{Target: target}},
		"no target":  {{BindingName: "ownProfile"}},
		"duplicate":  {{BindingName: "ownProfile", Target: target}, {BindingName: "ownProfile", Target: target}},
	} {
		_, err := NewProfileTriggerRunner(ProfileTriggerRunnerConfig{Connection: connection, ProfileChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewProfileTriggerRunner(ProfileTriggerRunnerConfig{ProfileChangedRoutes: []ProfileChangedTriggerRoute{{BindingName: "ownProfile", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableProfileTriggerRunnerCommitsEachChangeAfterRecordingIt(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	connection := newTriggerTestConnection(t, fake.UserInfoURL(), linkedinprofilefake.AccessToken)
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[ProfileChangedEvent], 8)
	// No saved binding: the empty configuration applies.
	runner, err := newDurableProfileTriggerRunner(connection, projectconfig.Configuration{}, storage, ProjectProfileTriggerRunnerConfig{
		ProfileChangedRoutes: []ProjectProfileChangedTriggerRoute{{BindingName: "ownProfile", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerTestConnectionName, TriggerName: "profileChanged", BindingName: "ownProfile"}
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
	require.Contains(t, committed().Token, `"subject":"782bbtaQ"`)

	fake.Update(func(profile *linkedinprofilefake.Profile) { profile.Email = "ada.king@example.com" })
	runner.WakePoller("ownProfile")
	event := receiveProfileChange(t, events)
	require.Equal(t, []ProfileField{ProfileFieldEmail}, event.Payload.ChangedFields)
	require.Equal(t, "ada.king@example.com", event.Payload.Current.Email)
	require.Eventually(t, func() bool {
		state := committed()
		return state != nil && state.CommitCount == 2
	}, 10*time.Second, 10*time.Millisecond)
	require.Contains(t, committed().Token, `"sequence":1`)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedProfileChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	fake := linkedinprofilefake.New(t, linkedinprofilefake.AdaProfile())
	connection := newTriggerTestConnection(t, fake.UserInfoURL(), linkedinprofilefake.AccessToken)
	// A short interval keeps the test fast; New rejects one under the 1m minimum.
	connection.client.pollInterval = time.Second
	events := make(chan sdkgo.TriggerEvent[ProfileChangedEvent], 4)
	trigger := NewProfileChangedTrigger(ProfileChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerTestConnectionName, BindingName: "ownProfile", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return fake.RequestCount() > 0 }, 10*time.Second, 10*time.Millisecond)
	fake.Update(func(profile *linkedinprofilefake.Profile) { profile.GivenName = "Augusta Ada" })

	event := receiveProfileChange(t, events)
	require.Equal(t, []ProfileField{ProfileFieldGivenName}, event.Payload.ChangedFields)
	require.Equal(t, "Ada", event.Payload.Previous.GivenName)
}
