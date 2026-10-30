// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// projectConfiguration is the project configuration Dex Web saves for one webhook connection and its bindings.
func projectConfiguration(bindings map[string]string) projectconfig.Configuration {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: webhook.ConnectorID, ConnectionName: testConnection.Name,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook", Provider: "webhook",
		Configuration: json.RawMessage(`{"eventIdPointer":"/event_id"}`),
	}}}
	for bindingName, bindingConfiguration := range bindings {
		configuration.TriggerBindings = append(configuration.TriggerBindings, projectconfig.TriggerConfiguration{
			ConnectorID: webhook.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "requestReceived",
			BindingName: bindingName, Configuration: json.RawMessage(bindingConfiguration),
		})
	}
	return configuration
}

// recordingInboxes stands in for durable project inboxes, recording each inbox key and every prepared event.
type recordingInboxes struct {
	mu       sync.Mutex
	keys     []projectconfig.TriggerInboxKey
	prepared []sdkgo.TriggerEvent[webhook.WebhookRequestEvent]
}

func (inboxes *recordingInboxes) wrap(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[webhook.WebhookRequestEvent],
) (sdkgo.TriggerTarget[webhook.WebhookRequestEvent], error) {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	inboxes.keys = append(inboxes.keys, key)
	return recordingTarget{inboxes: inboxes, target: target}, nil
}

func (inboxes *recordingInboxes) preparedEvents() []sdkgo.TriggerEvent[webhook.WebhookRequestEvent] {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	return slices.Clone(inboxes.prepared)
}

// recordingTarget records an event when the endpoint prepares it and passes deliveries to the application target.
type recordingTarget struct {
	inboxes *recordingInboxes
	target  sdkgo.TriggerTarget[webhook.WebhookRequestEvent]
}

func (target recordingTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error {
	target.inboxes.mu.Lock()
	defer target.inboxes.mu.Unlock()
	target.inboxes.prepared = append(target.inboxes.prepared, event)
	return nil
}

func (target recordingTarget) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error {
	return target.target.HandleTrigger(ctx, event)
}

func TestEndpointRunnerRecordsBeforeAcknowledgingAndAnswers503UntilRunning(t *testing.T) {
	inboxes := &recordingInboxes{}
	release := make(chan struct{})
	handled := make(chan string, 4)
	connection := newTestConnection(t, webhook.Config{EventIDPointer: "/event_id"}, sentinelSecret)
	runner, err := webhook.NewRequestReceivedEndpointRunnerForTest(connection, projectConfiguration(map[string]string{"submissions": `{}`}),
		[]webhook.ProjectRequestReceivedTriggerRoute{{
			BindingName: "submissions",
			Target: sdkgo.TriggerTargetFunc[webhook.WebhookRequestEvent](func(ctx context.Context, event sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error {
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
				handled <- event.ID
				return nil
			}),
		}}, inboxes.wrap)
	require.NoError(t, err)
	require.Equal(t, []projectconfig.TriggerInboxKey{{
		ConnectorID: webhook.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "requestReceived", BindingName: "submissions",
	}}, inboxes.keys, "every route's target is wrapped in its binding's durable inbox")
	server := httptest.NewServer(runner)
	t.Cleanup(server.Close)
	post := func() int {
		request, err := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(submissionBody))
		require.NoError(t, err)
		request.Header.Set("Content-Type", webhook.ContentTypeJSON)
		request.Header.Set("X-Signature-256", hexSignature(sentinelSecret, submissionBody))
		response, err := server.Client().Do(request)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		return response.StatusCode
	}
	require.Equal(t, http.StatusServiceUnavailable, post(), "no binding runs yet, so the sender retries")
	require.Zero(t, runner.RunningSourceCount())

	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, http.StatusOK, post())
	prepared := inboxes.preparedEvents()
	require.Len(t, prepared, 1, "the 200 came after the event was recorded")
	require.Equal(t, "evt_1", prepared[0].ID)
	stored, err := json.Marshal(prepared[0])
	require.NoError(t, err)
	require.NotContains(t, string(stored), sentinelSecret)
	close(release)
	select {
	case eventID := <-handled:
		require.Equal(t, "evt_1", eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the recorded event was not delivered")
	}
}

func TestEndpointRunnerRejectsIncompleteRoutes(t *testing.T) {
	target := sdkgo.TriggerTargetFunc[webhook.WebhookRequestEvent](func(context.Context, sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error { return nil })
	connection := newTestConnection(t, webhook.Config{EventIDPointer: "/event_id"}, sentinelSecret)
	configuration := projectConfiguration(map[string]string{
		"submissions": `{"matchPointer":"/event_type","matchValues":["form_response"]}`,
		"invalid":     `{"matchPointer":"/event_type"}`,
		"unknown":     `{"matchField":"event_type"}`,
	})
	inboxes := &recordingInboxes{}
	_, err := webhook.NewRequestReceivedEndpointRunnerForTest(connection, configuration, []webhook.ProjectRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	}, inboxes.wrap)
	require.NoError(t, err)
	for name, routes := range map[string][]webhook.ProjectRequestReceivedTriggerRoute{
		"no routes":          nil,
		"unstored binding":   {{BindingName: "missing", Target: target}},
		"invalid binding":    {{BindingName: "invalid", Target: target}},
		"unknown member":     {{BindingName: "unknown", Target: target}},
		"blank binding name": {{BindingName: " ", Target: target}},
		"nil target":         {{BindingName: "submissions"}},
		"duplicated binding": {{BindingName: "submissions", Target: target}, {BindingName: "submissions", Target: target}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := webhook.NewRequestReceivedEndpointRunnerForTest(connection, configuration, routes, inboxes.wrap)
			require.Error(t, err)
		})
	}
	_, err = webhook.NewRequestReceivedEndpointRunnerForTest(connection, configuration, []webhook.ProjectRequestReceivedTriggerRoute{
		{BindingName: "missing", Target: target},
	}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)

	client, err := webhook.New(webhook.Config{EventIDPointer: "/event_id"}, staticCredentials(sentinelSecret))
	require.NoError(t, err)
	anotherConnection, err := webhook.NewConnection(client, sdkgo.ConnectionRef{Provider: "webhook", Name: "another-connection"})
	require.NoError(t, err)
	_, err = webhook.NewRequestReceivedEndpointRunnerForTest(anotherConnection, configuration, []webhook.ProjectRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound, "bindings belong to the connection they name")

	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = webhook.NewRequestReceivedEndpointRunnerForTest(connection, configuration, []webhook.ProjectRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	}, func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[webhook.WebhookRequestEvent]) (sdkgo.TriggerTarget[webhook.WebhookRequestEvent], error) {
		return nil, inboxFailure
	})
	require.ErrorIs(t, err, inboxFailure)
	_, err = webhook.NewProjectRequestReceivedEndpointRunner(nil, testConnection.Name, []webhook.ProjectRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	})
	require.Error(t, err)
}
