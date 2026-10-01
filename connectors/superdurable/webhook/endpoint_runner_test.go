// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webhook_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// writeLocalStore writes a Dex Web style connection file with one webhook connection and its bindings.
func writeLocalStore(t *testing.T, bindings map[string]any) (*localconfig.Store, string) {
	t.Helper()
	directory := t.TempDir()
	triggerBindings := []any{}
	for bindingName, configuration := range bindings {
		triggerBindings = append(triggerBindings, map[string]any{
			"connectorId": webhook.ConnectorID, "connectionName": testConnection.Name, "triggerName": "requestReceived",
			"bindingName": bindingName, "configuration": configuration,
		})
	}
	contents, err := json.Marshal(map[string]any{
		"schemaVersion": localconfig.SchemaVersion,
		"connections": []any{map[string]any{
			"connectorId": webhook.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook",
			"moduleVersion": "v0.1.0", "provider": "webhook", "connectionName": testConnection.Name,
			"configuration": map[string]any{"eventIdPointer": "/event_id"},
			"credentials":   map[string]any{"signing_secret": sentinelSecret},
		}},
		"triggerBindings": triggerBindings,
	})
	require.NoError(t, err)
	path := filepath.Join(directory, "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	return store, directory
}

func TestLocalEndpointRunnerRecordsBeforeAcknowledgingAndAnswers503UntilRunning(t *testing.T) {
	store, directory := writeLocalStore(t, map[string]any{"submissions": map[string]any{}})
	release := make(chan struct{})
	handled := make(chan string, 4)
	runner, err := webhook.NewLocalRequestReceivedEndpointRunner(store, testConnection.Name, []webhook.LocalRequestReceivedTriggerRoute{{
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
	}})
	require.NoError(t, err)
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
	inboxes, err := filepath.Glob(filepath.Join(directory, ".trigger-inbox-*.json"))
	require.NoError(t, err)
	require.Len(t, inboxes, 1, "the 200 came after the event was on disk")
	inbox, err := os.ReadFile(inboxes[0])
	require.NoError(t, err)
	require.Contains(t, string(inbox), `"eventId":"evt_1"`)
	require.NotContains(t, string(inbox), sentinelSecret)
	close(release)
	select {
	case eventID := <-handled:
		require.Equal(t, "evt_1", eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the recorded event was not delivered")
	}
}

func TestNewLocalEndpointRunnerRejectsIncompleteRoutes(t *testing.T) {
	target := sdkgo.TriggerTargetFunc[webhook.WebhookRequestEvent](func(context.Context, sdkgo.TriggerEvent[webhook.WebhookRequestEvent]) error { return nil })
	store, _ := writeLocalStore(t, map[string]any{
		"submissions": map[string]any{"matchPointer": "/event_type", "matchValues": []string{"form_response"}},
		"invalid":     map[string]any{"matchPointer": "/event_type"},
		"unknown":     map[string]any{"matchField": "event_type"},
	})
	_, err := webhook.NewLocalRequestReceivedEndpointRunner(store, testConnection.Name, []webhook.LocalRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	})
	require.NoError(t, err)
	for name, routes := range map[string][]webhook.LocalRequestReceivedTriggerRoute{
		"no routes":          nil,
		"unstored binding":   {{BindingName: "missing", Target: target}},
		"invalid binding":    {{BindingName: "invalid", Target: target}},
		"unknown member":     {{BindingName: "unknown", Target: target}},
		"blank binding name": {{BindingName: " ", Target: target}},
		"nil target":         {{BindingName: "submissions"}},
		"duplicated binding": {{BindingName: "submissions", Target: target}, {BindingName: "submissions", Target: target}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := webhook.NewLocalRequestReceivedEndpointRunner(store, testConnection.Name, routes)
			require.Error(t, err)
		})
	}
	_, err = webhook.NewLocalRequestReceivedEndpointRunner(store, "another-connection", []webhook.LocalRequestReceivedTriggerRoute{
		{BindingName: "submissions", Target: target},
	})
	require.Error(t, err)
}
