// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go/internal/jirasearchfake"
	"github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go/internal/testsupport"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

var triggerTestScope = projectconfig.Scope{ProjectID: "jira-trigger-test", Kind: "live"}

// objectTriggerStorage opens inboxes and cursors in one in-memory store, as a loaded project does in S3.
type objectTriggerStorage struct{ objects *testsupport.ObjectStore }

func (storage objectTriggerStorage) TriggerInbox(key projectconfig.TriggerInboxKey) (*projectconfig.TriggerInbox, error) {
	return projectconfig.NewTriggerInbox(storage.objects, triggerTestScope, key)
}

func (storage objectTriggerStorage) PollCursorStore(key projectconfig.PollCursorKey) (*projectconfig.PollCursorStore, error) {
	return projectconfig.NewPollCursorStore(storage.objects, triggerTestScope, key)
}

// newRealTimeTriggerConnection uses the wall clock on both sides, as a running Worker does.
func newRealTimeTriggerConnection(t *testing.T, site *jirasearchfake.Site) Connection {
	t.Helper()
	client, err := New(Config{CloudID: jirasearchfake.CloudID, Endpoint: site.URL, PollInterval: minimumPollInterval}, sdkgo.StaticCredentialProvider[Credentials]{
		triggerTestConnection: {
			OAuthClientID: "client-id", OAuthClientSecret: sdkgo.NewSecretString("client-secret"),
			AccessToken: sdkgo.NewSecretString("jira-trigger-token"), RefreshToken: sdkgo.NewSecretString("refresh-token"),
		},
	})
	require.NoError(t, err)
	connection, err := NewConnection(client, triggerTestConnection)
	require.NoError(t, err)
	return connection
}

func channelTarget(events chan sdkgo.TriggerEvent[IssueChangedEvent]) sdkgo.TriggerTarget[IssueChangedEvent] {
	return sdkgo.TriggerTargetFunc[IssueChangedEvent](func(_ context.Context, event sdkgo.TriggerEvent[IssueChangedEvent]) error {
		events <- event
		return nil
	})
}

func TestNewIssueTriggerRunnerRejectsIncompleteRoutes(t *testing.T) {
	connection := newRealTimeTriggerConnection(t, jirasearchfake.New(t))
	target := channelTarget(make(chan sdkgo.TriggerEvent[IssueChangedEvent], 1))
	for name, routes := range map[string][]IssueChangedTriggerRoute{
		"none":        nil,
		"no binding":  {{Target: target}},
		"no target":   {{BindingName: "opsIssues"}},
		"duplicate":   {{BindingName: "opsIssues", Target: target}, {BindingName: "opsIssues", Target: target}},
		"bad project": {{BindingName: "opsIssues", Target: target, Configuration: IssueChangedTriggerConfiguration{ProjectKey: "ops"}}},
	} {
		_, err := NewIssueTriggerRunner(IssueTriggerRunnerConfig{Connection: connection, IssueChangedRoutes: routes})
		require.Error(t, err, name)
	}
	_, err := NewIssueTriggerRunner(IssueTriggerRunnerConfig{IssueChangedRoutes: []IssueChangedTriggerRoute{{BindingName: "opsIssues", Target: target}}})
	require.Error(t, err, "a zero Connection is rejected")
}

func TestDurableIssueTriggerRunnerLoadsTheSavedProjectAndCommitsAfterRecording(t *testing.T) {
	site := jirasearchfake.New(t)
	site.Create(jirasearchfake.Operations, "Existing")
	connection := newRealTimeTriggerConnection(t, site)
	saved, err := json.Marshal(IssueChangedTriggerConfiguration{ProjectID: "10000", ProjectKey: "OPS", ProjectName: "Operations"})
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "issueChanged", BindingName: "opsIssues", Configuration: saved,
	}}}
	storage := objectTriggerStorage{objects: testsupport.NewObjectStore()}
	events := make(chan sdkgo.TriggerEvent[IssueChangedEvent], 4)
	runner, err := newDurableIssueTriggerRunner(connection, configuration, storage, ProjectIssueTriggerRunnerConfig{
		IssueChangedRoutes: []ProjectIssueChangedTriggerRoute{{BindingName: "opsIssues", Target: channelTarget(events)}},
	})
	require.NoError(t, err)
	key := projectconfig.TriggerInboxKey{ConnectorID: ConnectorID, ConnectionName: triggerTestConnection.Name, TriggerName: "issueChanged", BindingName: "opsIssues"}
	cursor, err := storage.PollCursorStore(projectconfig.PollCursorKey(key))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- runner.Run(ctx) }()
	t.Cleanup(func() { cancel(); require.ErrorIs(t, <-result, context.Canceled) })

	committed := func() issueChangeCursor {
		record, err := cursor.ReadPollCursor(context.Background())
		if err != nil || record.State == nil {
			return issueChangeCursor{}
		}
		var position issueChangeCursor
		if json.Unmarshal([]byte(record.State.Token), &position) != nil {
			return issueChangeCursor{}
		}
		return position
	}
	require.Eventually(t, func() bool { return committed().CloudID != "" }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, "id:10000", committed().Project, "the saved project ID selects the watched project")
	site.Create(jirasearchfake.HumanResources, "Other project")
	created := site.Create(jirasearchfake.Operations, "Loading dock light out")
	runner.WakePoller("opsIssues")

	select {
	case event := <-events:
		require.Equal(t, created, event.Payload.Issue.Key, "the baseline and other projects are never delivered")
		require.Equal(t, IssueChangeCreated, event.Payload.ChangeKind)
	case <-time.After(10 * time.Second):
		t.Fatal("the new issue was not delivered")
	}
	require.Eventually(t, func() bool { return len(committed().SeenVersions) == 2 }, 10*time.Second, 10*time.Millisecond)
	inbox, err := storage.TriggerInbox(key)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		pending, err := inbox.Pending(context.Background())
		return err == nil && len(pending) == 0
	}, 10*time.Second, 10*time.Millisecond)
}

func TestGeneratedIssueChangedTriggerBaselinesAndDeliversTheNextChange(t *testing.T) {
	site := jirasearchfake.New(t)
	existing := site.Create(jirasearchfake.Operations, "Existing")
	connection := newRealTimeTriggerConnection(t, site)
	events := make(chan sdkgo.TriggerEvent[IssueChangedEvent], 4)
	trigger := NewIssueChangedTrigger(IssueChangedTriggerConfig{
		Connection: connection, ConnectionName: triggerTestConnection.Name, BindingName: "allIssues", Target: channelTarget(events),
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- trigger.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-result })
	require.Eventually(t, func() bool { return len(site.Requests()) > 0 }, 10*time.Second, 10*time.Millisecond)
	require.Equal(t, "updated >= -4m ORDER BY updated DESC", site.Requests()[0].JQL, "a blank project watches every browsable project")
	time.Sleep(5 * time.Millisecond) // The update must carry a later millisecond than the baseline issue.
	site.Update(existing, "Existing, edited")

	select {
	case event := <-events:
		require.Equal(t, "Existing, edited", event.Payload.Issue.Summary, "the edited version is new; the baseline version is not delivered")
		require.Equal(t, jirasearchfake.CloudID, event.Payload.CloudID)
	case <-time.After(20 * time.Second):
		t.Fatal("the generated Trigger did not deliver the change after one poll interval")
	}
}
