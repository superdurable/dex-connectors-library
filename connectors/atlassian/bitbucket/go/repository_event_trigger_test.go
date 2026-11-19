// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

func TestRepositoryEventWebhookVerifiesSignatureAndDecodesPullRequest(t *testing.T) {
	body := []byte(`{"repository":{"uuid":"{a1}","full_name":"meridian/api"},"pullrequest":{"id":7}}`)
	mac := hmac.New(sha256.New, []byte("test-webhook-secret"))
	_, err := mac.Write(body)
	require.NoError(t, err)
	headers := http.Header{}
	headers.Set("X-Hub-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	headers.Set("X-Event-Key", PullRequestCreatedEventKey)
	headers.Set("X-Request-UUID", "31cd9dd9-35f3-4c53-b039-7a92e8d96c31")
	request := webhooktrigger.Request{Body: body, Header: headers, ReceivedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	webhookSecret := sdkgo.NewSecretString("test-webhook-secret")
	require.NoError(t, verifyRepositoryEventRequest(request, webhookSecret))
	event, accepted, err := decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Equal(t, "31cd9dd9-35f3-4c53-b039-7a92e8d96c31:pullrequest:created", event.ID)
	require.Equal(t, int64(7), event.Payload.PullRequestID)
	require.Equal(t, "meridian/api", event.Payload.RepositoryFullName)
	require.Equal(t, request.ReceivedAt, event.OccurredAt)
	headers.Set("X-Hub-Signature", "sha256="+hex.EncodeToString(make([]byte, sha256.Size)))
	require.ErrorIs(t, verifyRepositoryEventRequest(request, webhookSecret), errRepositoryEventSignatureInvalid)
	require.ErrorIs(t, verifyRepositoryEventRequest(request, sdkgo.SecretString{}), webhooktrigger.ErrVerificationUnavailable)
}

func TestRepositoryPushWebhookKeepsEveryBranchChange(t *testing.T) {
	request := webhooktrigger.Request{Body: []byte(`{"repository":{"uuid":"{a1}","full_name":"meridian/api"},"push":{"changes":[{"old":{"type":"branch","name":"main","target":{"hash":"111"}},"new":{"type":"branch","name":"main","target":{"hash":"222"}}},{"new":{"type":"branch","name":"feature/tests","target":{"hash":"333"}}}]}}`), Header: http.Header{}, ReceivedAt: time.Now()}
	request.Header.Set("X-Event-Key", RepositoryPushEventKey)
	request.Header.Set("X-Request-UUID", "31cd9dd9-35f3-4c53-b039-7a92e8d96c31")
	event, accepted, err := decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Len(t, event.Payload.PushChanges, 2)
	require.Equal(t, "main", event.Payload.PushChanges[0].OldBranch)
	require.Equal(t, "222", event.Payload.PushChanges[0].NewTargetHash)
	require.Equal(t, "feature/tests", event.Payload.PushChanges[1].NewBranch)
	request.Header.Set("X-Event-Key", "unknown:event")
	_, accepted, err = decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.False(t, accepted)
}

func TestRepositoryCommentWebhookIdentifiesPullRequestAndComment(t *testing.T) {
	request := webhooktrigger.Request{Body: []byte(`{"repository":{"uuid":"{a1}","full_name":"meridian/api"},"pullrequest":{"id":7},"comment":{"id":3}}`), Header: http.Header{}, ReceivedAt: time.Now()}
	request.Header.Set("X-Event-Key", PullRequestCommentResolvedEventKey)
	request.Header.Set("X-Request-UUID", "31cd9dd9-35f3-4c53-b039-7a92e8d96c31")
	event, accepted, err := decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Equal(t, int64(7), event.Payload.PullRequestID)
	require.Equal(t, int64(3), event.Payload.CommentID)
}

func TestRepositoryEventTriggerConfigurationRejectsUnsupportedOrDuplicateKeys(t *testing.T) {
	require.NoError(t, (RepositoryEventReceivedTriggerConfiguration{}).Validate())
	require.Error(t, (RepositoryEventReceivedTriggerConfiguration{EventKeys: []string{RepositoryPushEventKey, RepositoryPushEventKey}}).Validate())
	require.Error(t, (RepositoryEventReceivedTriggerConfiguration{EventKeys: []string{"unknown:event"}}).Validate())
}

func TestWorkspaceProjectEventAndTagPush(t *testing.T) {
	request := webhooktrigger.Request{Body: []byte(`{"workspace":{"slug":"meridian"},"project":{"key":"API"}}`), Header: http.Header{}, ReceivedAt: time.Now()}
	request.Header.Set("X-Event-Key", "project:updated")
	request.Header.Set("X-Request-UUID", "31cd9dd9-35f3-4c53-b039-7a92e8d96c31")
	event, accepted, err := decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Equal(t, "meridian", event.Payload.WorkspaceSlug)
	require.Equal(t, "API", event.Payload.ProjectKey)
	request.Body = []byte(`{"repository":{"uuid":"{a1}","full_name":"meridian/api"},"push":{"changes":[{"new":{"type":"tag","name":"v1.0.0","target":{"hash":"333"}}}]}}`)
	request.Header.Set("X-Event-Key", RepositoryPushEventKey)
	event, accepted, err = decodeRepositoryEventRequest(request)
	require.NoError(t, err)
	require.True(t, accepted)
	require.Equal(t, "v1.0.0", event.Payload.PushChanges[0].NewTag)
}

func TestRepositoryEventRunnerLoadsTheConfiguredBinding(t *testing.T) {
	client := newTestClient(t, "https://api.bitbucket.org/2.0")
	connection, err := NewConnection(client, testConnection)
	require.NoError(t, err)
	configuration := projectconfig.Configuration{TriggerBindings: []projectconfig.TriggerConfiguration{{
		ConnectorID: ConnectorID, ConnectionName: testConnection.Name, TriggerName: "repositoryEventReceived", BindingName: "repository-events",
		Configuration: json.RawMessage(`{"eventKeys":["repo:push"]}`),
	}}}
	target := sdkgo.TriggerTargetFunc[RepositoryEvent](func(context.Context, sdkgo.TriggerEvent[RepositoryEvent]) error { return nil })
	runner, err := newRepositoryEventReceivedEndpointRunner(connection, configuration, []ProjectRepositoryEventReceivedTriggerRoute{{BindingName: "repository-events", Target: target}}, func(_ projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[RepositoryEvent]) (sdkgo.TriggerTarget[RepositoryEvent], error) {
		return target, nil
	})
	require.NoError(t, err)
	require.NotNil(t, runner)
	require.Equal(t, 0, runner.RunningSourceCount())
}
