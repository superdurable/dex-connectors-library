// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

// projectConfiguration is the project configuration Dex Web saves for one Typeform connection and its bindings.
func projectConfiguration(bindings map[string]string) projectconfig.Configuration {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: typeform.ConnectorID, ConnectionName: testConnection.Name,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/typeform", Provider: "typeform",
		AuthMethodID: typeform.PersonalAccessTokenAuthMethodID, Configuration: json.RawMessage(`{}`),
	}}}
	for bindingName, bindingConfiguration := range bindings {
		configuration.TriggerBindings = append(configuration.TriggerBindings, projectconfig.TriggerConfiguration{
			ConnectorID: typeform.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "responseSubmitted",
			BindingName: bindingName, Configuration: json.RawMessage(bindingConfiguration),
		})
	}
	return configuration
}

// recordingInboxes stands in for durable project inboxes, recording each inbox key and every prepared event.
type recordingInboxes struct {
	mu       sync.Mutex
	keys     []projectconfig.TriggerInboxKey
	prepared []sdkgo.TriggerEvent[typeform.FormResponseEvent]
}

func (inboxes *recordingInboxes) wrap(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[typeform.FormResponseEvent],
) (sdkgo.TriggerTarget[typeform.FormResponseEvent], error) {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	inboxes.keys = append(inboxes.keys, key)
	return recordingTarget{inboxes: inboxes, target: target}, nil
}

func (inboxes *recordingInboxes) preparedEvents() []sdkgo.TriggerEvent[typeform.FormResponseEvent] {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	return slices.Clone(inboxes.prepared)
}

// recordingTarget records an event when the endpoint prepares it and passes deliveries to the application target.
type recordingTarget struct {
	inboxes *recordingInboxes
	target  sdkgo.TriggerTarget[typeform.FormResponseEvent]
}

func (target recordingTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[typeform.FormResponseEvent]) error {
	target.inboxes.mu.Lock()
	defer target.inboxes.mu.Unlock()
	target.inboxes.prepared = append(target.inboxes.prepared, event)
	return nil
}

func (target recordingTarget) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[typeform.FormResponseEvent]) error {
	return target.target.HandleTrigger(ctx, event)
}

func TestEndpointRunnerRecordsBeforeAcknowledgingAndAnswers503UntilRunning(t *testing.T) {
	inboxes := &recordingInboxes{}
	handled := make(chan string, 4)
	connection, fixture := newIdleWebhookFixture(t, typeform.Config{}, sentinelSecret)
	runner, err := typeform.NewResponseSubmittedEndpointRunnerForTest(connection,
		projectConfiguration(map[string]string{"submissions": `{"formId":"` + testFormID + `"}`}),
		[]typeform.ProjectResponseSubmittedTriggerRoute{{
			BindingName: "submissions",
			Target: sdkgo.TriggerTargetFunc[typeform.FormResponseEvent](func(_ context.Context, event sdkgo.TriggerEvent[typeform.FormResponseEvent]) error {
				handled <- event.ID
				return nil
			}),
		}}, inboxes.wrap)
	require.NoError(t, err)
	require.Equal(t, []projectconfig.TriggerInboxKey{{
		ConnectorID: typeform.ConnectorID, ConnectionName: testConnection.Name, TriggerName: "responseSubmitted", BindingName: "submissions",
	}}, inboxes.keys, "every route's target is wrapped in its binding's durable inbox")
	fixture.handler = runner
	body := webhookBody(t, typeform.WebhookEventTypeFormResponse, testFormID, "a3a12ec67a1365927098a606107fac15")
	require.Equal(t, http.StatusServiceUnavailable, fixture.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)),
		"no binding runs yet, so Typeform retries")
	require.Zero(t, runner.RunningSourceCount())

	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	require.Equal(t, http.StatusOK, fixture.deliver(t, http.MethodPost, body, typeformSignature(body, sentinelSecret)))
	prepared := inboxes.preparedEvents()
	require.Len(t, prepared, 1, "the 200 came after the submission was recorded")
	require.Equal(t, testFormID+":a3a12ec67a1365927098a606107fac15", prepared[0].ID)
	stored, err := json.Marshal(prepared[0])
	require.NoError(t, err)
	require.NotContains(t, string(stored), sentinelSecret)
	require.NotContains(t, string(stored), sentinelToken)
	select {
	case eventID := <-handled:
		require.Equal(t, prepared[0].ID, eventID)
	case <-time.After(5 * time.Second):
		t.Fatal("the recorded submission was not delivered")
	}
}

func TestEndpointRunnerRejectsIncompleteRoutes(t *testing.T) {
	target := sdkgo.TriggerTargetFunc[typeform.FormResponseEvent](func(context.Context, sdkgo.TriggerEvent[typeform.FormResponseEvent]) error { return nil })
	connection, _ := newIdleWebhookFixture(t, typeform.Config{}, sentinelSecret)
	configuration := projectConfiguration(map[string]string{
		"submissions": `{"formId":"` + testFormID + `"}`,
		"invalid":     `{"formId":"not a form ID"}`,
		"unknown":     `{"form":"` + testFormID + `"}`,
	})
	inboxes := &recordingInboxes{}
	_, err := typeform.NewResponseSubmittedEndpointRunnerForTest(connection, configuration, []typeform.ProjectResponseSubmittedTriggerRoute{
		{BindingName: "submissions", Target: target},
	}, inboxes.wrap)
	require.NoError(t, err)
	for name, routes := range map[string][]typeform.ProjectResponseSubmittedTriggerRoute{
		"no routes":          nil,
		"unstored binding":   {{BindingName: "missing", Target: target}},
		"invalid binding":    {{BindingName: "invalid", Target: target}},
		"unknown member":     {{BindingName: "unknown", Target: target}},
		"blank binding name": {{BindingName: " ", Target: target}},
		"nil target":         {{BindingName: "submissions"}},
		"duplicated binding": {{BindingName: "submissions", Target: target}, {BindingName: "submissions", Target: target}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := typeform.NewResponseSubmittedEndpointRunnerForTest(connection, configuration, routes, inboxes.wrap)
			require.Error(t, err)
		})
	}
	_, err = typeform.NewResponseSubmittedEndpointRunnerForTest(connection, configuration, []typeform.ProjectResponseSubmittedTriggerRoute{
		{BindingName: "missing", Target: target},
	}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)

	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = typeform.NewResponseSubmittedEndpointRunnerForTest(connection, configuration, []typeform.ProjectResponseSubmittedTriggerRoute{
		{BindingName: "submissions", Target: target},
	}, func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[typeform.FormResponseEvent]) (sdkgo.TriggerTarget[typeform.FormResponseEvent], error) {
		return nil, inboxFailure
	})
	require.ErrorIs(t, err, inboxFailure)
	_, err = typeform.NewProjectResponseSubmittedEndpointRunner(nil, testConnection.Name, []typeform.ProjectResponseSubmittedTriggerRoute{
		{BindingName: "submissions", Target: target},
	})
	require.Error(t, err)
}
