// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mailchimp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go/internal/mailchimpaudiencefake"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "mailchimp-trigger-test", Kind: "live"}

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
	client, err := New(Config{PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		fetcherTestConnection: {APIKey: sdkgo.NewSecretString(mailchimpaudiencefake.APIKey)},
	}, WithAPIBaseURL(baseURL))
	require.NoError(t, err)
	connection, err := NewConnection(client, fetcherTestConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[MemberChangedEvent]) sdkgo.TriggerTarget[MemberChangedEvent] {
	return sdkgo.TriggerTargetFunc[MemberChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[MemberChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewMemberTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/3.0")
	target := channelTarget(make(chan sdkgo.TriggerEvent[MemberChangedEvent], 1))
	audience := MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID}
	for name, routes := range map[string][]MemberChangedTriggerRoute{
		"none":        nil,
		"no binding":  {{Target: target, Configuration: audience}},
		"no target":   {{BindingName: "audienceChanges", Configuration: audience}},
		"duplicate":   {{BindingName: "audienceChanges", Target: target, Configuration: audience}, {BindingName: "audienceChanges", Target: target, Configuration: audience}},
		"no audience": {{BindingName: "audienceChanges", Target: target}},
	} {
		_, err := NewMemberTriggerRunner(MemberTriggerRunnerConfig{Connection: connection, MemberChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := New(Config{PollInterval: time.Second}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.ErrorContains(t, err, "at least 5s")
}

func TestDurableMemberTriggerRunnerRequiresASavedAudience(t *testing.T) {
	connection := newTriggerTestConnection(t, "http://127.0.0.1:1/3.0")
	_, err := newDurableMemberTriggerRunner(connection, projectconfig.Configuration{}, objectTriggerStorage{objects: testsupport.NewObjectStore()}, ProjectMemberTriggerRunnerConfig{
		MemberChangedRoutes: []ProjectMemberChangedTriggerRoute{{BindingName: "audienceChanges", Target: channelTarget(make(chan sdkgo.TriggerEvent[MemberChangedEvent], 1))}},
	})
	require.ErrorContains(t, err, "has no saved audience")
}

func TestDurableMemberTriggerRunnerLoadsTheSavedAudienceAndCommitsAfterRecording(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	connection := newTriggerTestConnection(t, audience.URL)
	saved, err := json.Marshal(MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID, Statuses: []MemberStatus{MemberStatusSubscribed}})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: fetcherTestConnection.Name, TriggerName: "memberChanged", BindingName: "audienceChanges", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[MemberChangedEvent], 4)
	runner, err := newDurableMemberTriggerRunner(connection, configuration, storage, ProjectMemberTriggerRunnerConfig{
		MemberChangedRoutes: []ProjectMemberChangedTriggerRoute{{BindingName: "audienceChanges", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: fetcherTestConnection.Name, TriggerName: "memberChanged", BindingName: "audienceChanges"}
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
	require.JSONEq(t, `{"audienceId":"57afe96172","baseline":"2026-11-01T09:00:01Z","watermark":"2026-11-01T09:00:01Z"}`, committedToken())
	audience.Upsert("cara@example.com", "unsubscribed", "Cara")
	audience.Upsert("dana@example.com", "subscribed", "Dana")
	runner.WakePoller("audienceChanges")

	select {
	case event := <-events:
		require.Equal(t, "dana@example.com", event.Payload.Member.EmailAddress, "the baseline and the filtered status are never delivered")
		require.Equal(t, fetcherTestAudienceID, event.Payload.AudienceID)
	case <-time.After(10 * time.Second):
		t.Fatal("the new contact was not delivered")
	}
	require.Eventually(t, func() bool {
		return committedToken() == `{"audienceId":"57afe96172","baseline":"2026-11-01T09:00:01Z","watermark":"2026-11-01T09:00:03Z"}`
	}, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedMemberChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	audience := mailchimpaudiencefake.New(t, fetcherTestAudienceID)
	audience.Upsert("ben@example.com", "subscribed", "Ben")
	connection := newTriggerTestConnection(t, audience.URL)
	events := make(chan sdkgo.TriggerEvent[MemberChangedEvent], 4)
	trigger := NewMemberChangedTrigger(MemberChangedTriggerConfig{
		Connection: connection, ConnectionName: fetcherTestConnection.Name, BindingName: "audienceChanges",
		Configuration: MemberChangedTriggerConfiguration{AudienceID: fetcherTestAudienceID}, Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(audience.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	audience.Upsert("ben@example.com", "unsubscribed", "Ben")

	select {
	case event := <-events:
		require.Equal(t, MemberStatusUnsubscribed, event.Payload.Member.Status)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
