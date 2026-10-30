// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

const testWebhookID = "7fa3ec74-69a8-4530-a251-8a13730bd204"

func TestTaskEventEndpointVerifiesDecodesAndFiltersEvents(t *testing.T) {
	configuration := clickup.TaskEventTriggerConfiguration{Events: []string{clickup.EventTaskStatusUpdated, clickup.EventTaskDeleted}}
	endpoint, received := runTaskEventBinding(t, testCredentialProvider(), configuration)
	waitForBindingToReceive(t, endpoint, received, configuration)
	statusUpdated := statusUpdatedBody(testTaskID, "2800763136717140857", "in progress", "blocked")

	require.Equal(t, http.StatusMethodNotAllowed, serveTaskEvent(endpoint, http.MethodGet, nil, ""))
	require.Equal(t, http.StatusOK, serveTaskEvent(endpoint, http.MethodPost, statusUpdated, strings.ToUpper(signTaskEvent(statusUpdated, testWebhookSecret))))
	event := received.next(t)
	require.Equal(t, testWebhookID+":taskStatusUpdated:2800763136717140857", event.ID)
	require.Equal(t, time.UnixMilli(1642734631523).UTC(), event.OccurredAt)
	require.Equal(t, clickup.TaskEvent{
		EventID: event.ID, WebhookID: testWebhookID, Event: clickup.EventTaskStatusUpdated, TaskID: testTaskID, OccurredAt: event.OccurredAt,
		HistoryItems: []clickup.TaskHistoryItem{{
			ID: "2800763136717140857", Field: "status", OccurredAt: event.OccurredAt, UserID: 183, ParentID: "162641062",
			StatusBefore: "in progress", StatusAfter: "blocked",
		}},
	}, event.Payload)

	deleted := []byte(`{"event":"taskDeleted","task_id":"` + testTaskID + `","webhook_id":"` + testWebhookID + `"}`)
	require.Equal(t, http.StatusOK, serveTaskEvent(endpoint, http.MethodPost, deleted, signTaskEvent(deleted, testWebhookSecret)))
	deletedEvent := received.next(t)
	require.Equal(t, testWebhookID+":taskDeleted:"+testTaskID, deletedEvent.ID, "an event without history items is identified by its task")
	require.Empty(t, deletedEvent.Payload.HistoryItems)

	for name, request := range map[string]struct {
		body      []byte
		signature string
		status    int
	}{
		"wrong secret":            {body: statusUpdated, signature: signTaskEvent(statusUpdated, "another-secret"), status: http.StatusBadRequest},
		"missing signature":       {body: statusUpdated, status: http.StatusBadRequest},
		"sha1 signature":          {body: statusUpdated, signature: strings.Repeat("a", 40), status: http.StatusBadRequest},
		"tampered body":           {body: []byte(strings.Replace(string(statusUpdated), "blocked", "complete", 1)), signature: signTaskEvent(statusUpdated, testWebhookSecret), status: http.StatusBadRequest},
		"malformed JSON":          {body: []byte(`{"event":`), status: http.StatusBadRequest},
		"no webhook ID":           {body: []byte(`{"event":"taskCreated","task_id":"abc"}`), status: http.StatusBadRequest},
		"task event without task": {body: []byte(`{"event":"taskCreated","webhook_id":"` + testWebhookID + `"}`), status: http.StatusBadRequest},
		"list event":              {body: []byte(`{"event":"listCreated","list_id":"162641285","webhook_id":"` + testWebhookID + `"}`), status: http.StatusOK},
		"event the binding skips": {body: []byte(`{"event":"taskCommentPosted","task_id":"` + testTaskID + `","webhook_id":"` + testWebhookID + `","history_items":[{"id":"1","field":"comment"}]}`), status: http.StatusOK},
	} {
		signature := request.signature
		if signature == "" && name != "missing signature" {
			signature = signTaskEvent(request.body, testWebhookSecret)
		}
		require.Equal(t, request.status, serveTaskEvent(endpoint, http.MethodPost, request.body, signature), name)
	}
	received.requireNoMore(t)
}

func TestTaskEventEndpointAsksClickUpToRetryUntilItCanVerifyAndRecord(t *testing.T) {
	statusUpdated := statusUpdatedBody(testTaskID, "2800763136717140858", "to do", "blocked")
	withoutSecret, err := clickup.New(clickup.Config{}, sdkgo.StaticCredentialProvider[clickup.Credentials]{
		clickupConnection: {APIToken: sdkgo.NewSecretString(testAPIToken)},
	})
	require.NoError(t, err)
	connectionWithoutSecret, err := clickup.NewConnection(withoutSecret, clickupConnection)
	require.NoError(t, err)
	inboxes := &recordingInboxes{}
	runner, err := clickup.NewTaskEventEndpointRunnerForTest(connectionWithoutSecret, projectConfiguration(map[string]string{"test-binding": `{}`}),
		[]clickup.ProjectTaskEventTriggerRoute{{
			BindingName: "test-binding",
			Target: sdkgo.TriggerTargetFunc[clickup.TaskEvent](func(context.Context, sdkgo.TriggerEvent[clickup.TaskEvent]) error {
				t.Error("no event may be delivered without a webhook secret")
				return nil
			}),
		}}, inboxes.wrap)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(ctx) }()
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusServiceUnavailable, serveTaskEvent(runner, http.MethodPost, statusUpdated, signTaskEvent(statusUpdated, testWebhookSecret)),
		"a blank webhook_secret answers 503, never 401, which would suspend the webhook")
	require.Empty(t, inboxes.preparedEvents(), "an unverified event is not recorded")
	cancel()
	require.ErrorIs(t, <-runResult, context.Canceled)

	idle, err := newTestConnection(t).TaskEventWebhookHandler()
	require.NoError(t, err)
	require.Equal(t, http.StatusServiceUnavailable, serveTaskEvent(idle, http.MethodPost, statusUpdated, signTaskEvent(statusUpdated, testWebhookSecret)),
		"no running binding answers 503 so ClickUp retries")

	small, err := clickup.New(clickup.Config{WebhookMaxBodyBytes: 64}, testCredentialProvider())
	require.NoError(t, err)
	smallConnection, err := clickup.NewConnection(small, clickupConnection)
	require.NoError(t, err)
	smallHandler, err := smallConnection.TaskEventWebhookHandler()
	require.NoError(t, err)
	require.Equal(t, http.StatusRequestEntityTooLarge, serveTaskEvent(smallHandler, http.MethodPost, statusUpdated, signTaskEvent(statusUpdated, testWebhookSecret)))
}

func TestTaskEventTriggerConfigurationAcceptsOnlyTaskEvents(t *testing.T) {
	require.NoError(t, clickup.TaskEventTriggerConfiguration{}.Validate())
	require.NoError(t, clickup.TaskEventTriggerConfiguration{Events: clickup.TaskEvents()}.Validate())
	for name, events := range map[string][]string{
		"list event":     {"listCreated"},
		"wildcard":       {"*"},
		"duplicate":      {clickup.EventTaskCreated, clickup.EventTaskCreated},
		"different case": {"TaskCreated"},
	} {
		require.Error(t, clickup.TaskEventTriggerConfiguration{Events: events}.Validate(), name)
	}
	events := clickup.TaskEvents()
	events[0] = "mutated"
	require.Equal(t, clickup.EventTaskCreated, clickup.TaskEvents()[0], "callers get a copy")
}

// TestStudioEventPickerListsTheSupportedEvents keeps ui/src/events.ts equal to TaskEvents.
func TestStudioEventPickerListsTheSupportedEvents(t *testing.T) {
	contents, err := os.ReadFile("ui/src/events.ts")
	require.NoError(t, err)
	var uiEvents []string
	for _, match := range regexp.MustCompile(`\{event: "([^"]+)"`).FindAllStringSubmatch(string(contents), -1) {
		uiEvents = append(uiEvents, match[1])
	}
	require.Equal(t, clickup.TaskEvents(), uiEvents)
}

func TestEndpointRunnerRecordsBeforeAcknowledging(t *testing.T) {
	inboxes := &recordingInboxes{}
	received := &receivedEvents{events: make(chan sdkgo.TriggerEvent[clickup.TaskEvent], 4)}
	runner, err := clickup.NewTaskEventEndpointRunnerForTest(newTestConnection(t),
		projectConfiguration(map[string]string{"test-binding": `{"events":["` + clickup.EventTaskStatusUpdated + `"]}`}),
		[]clickup.ProjectTaskEventTriggerRoute{{BindingName: "test-binding", Target: received.target()}}, inboxes.wrap)
	require.NoError(t, err)
	require.Equal(t, []projectconfig.TriggerInboxKey{{
		ConnectorID: clickup.ConnectorID, ConnectionName: clickupConnection.Name, TriggerName: "taskEvent", BindingName: "test-binding",
	}}, inboxes.keys, "every route's target is wrapped in its binding's durable inbox")
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runResult, context.Canceled)
	})
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, 10*time.Millisecond)

	statusUpdated := statusUpdatedBody(testTaskID, "2800763136717140859", "to do", "blocked")
	require.Equal(t, http.StatusOK, serveTaskEvent(runner, http.MethodPost, statusUpdated, signTaskEvent(statusUpdated, testWebhookSecret)))
	prepared := inboxes.preparedEvents()
	require.Len(t, prepared, 1, "the 200 came after the event was recorded")
	stored, err := json.Marshal(prepared[0])
	require.NoError(t, err)
	require.NotContains(t, string(stored), testWebhookSecret)
	require.NotContains(t, string(stored), testAPIToken)
	require.Equal(t, testWebhookID+":taskStatusUpdated:2800763136717140859", received.next(t).ID)
}

func TestEndpointRunnerRejectsMissingOrInvalidBindings(t *testing.T) {
	target := sdkgo.TriggerTargetFunc[clickup.TaskEvent](func(context.Context, sdkgo.TriggerEvent[clickup.TaskEvent]) error { return nil })
	connection := newTestConnection(t)
	configuration := projectConfiguration(map[string]string{"test-binding": `{"events":["` + clickup.EventTaskCreated + `"]}`})
	inboxes := &recordingInboxes{}
	for name, routes := range map[string][]clickup.ProjectTaskEventTriggerRoute{
		"no routes":         nil,
		"blank binding":     {{BindingName: " ", Target: target}},
		"nil target":        {{BindingName: "test-binding"}},
		"duplicate binding": {{BindingName: "test-binding", Target: target}, {BindingName: "test-binding", Target: target}},
	} {
		_, err := clickup.NewTaskEventEndpointRunnerForTest(connection, configuration, routes, inboxes.wrap)
		require.Error(t, err, name)
	}
	_, err := clickup.NewTaskEventEndpointRunnerForTest(connection, configuration,
		[]clickup.ProjectTaskEventTriggerRoute{{BindingName: "another-binding", Target: target}}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	invalidEvents := projectConfiguration(map[string]string{"test-binding": `{"events":["listCreated"]}`})
	_, err = clickup.NewTaskEventEndpointRunnerForTest(connection, invalidEvents,
		[]clickup.ProjectTaskEventTriggerRoute{{BindingName: "test-binding", Target: target}}, inboxes.wrap)
	require.ErrorContains(t, err, "not a supported task event")
	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = clickup.NewTaskEventEndpointRunnerForTest(connection, configuration,
		[]clickup.ProjectTaskEventTriggerRoute{{BindingName: "test-binding", Target: target}},
		func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[clickup.TaskEvent]) (sdkgo.TriggerTarget[clickup.TaskEvent], error) {
			return nil, inboxFailure
		})
	require.ErrorIs(t, err, inboxFailure)
	_, err = clickup.NewProjectTaskEventEndpointRunner(nil, clickupConnection.Name,
		[]clickup.ProjectTaskEventTriggerRoute{{BindingName: "test-binding", Target: target}})
	require.Error(t, err)
}

// projectConfiguration is the project configuration Dex Web saves for the connection and its taskEvent bindings.
func projectConfiguration(bindings map[string]string) projectconfig.Configuration {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: clickup.ConnectorID, ConnectionName: clickupConnection.Name,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/clickup", Provider: "clickup",
		Configuration: json.RawMessage(`{}`),
	}}}
	for bindingName, bindingConfiguration := range bindings {
		configuration.TriggerBindings = append(configuration.TriggerBindings, projectconfig.TriggerConfiguration{
			ConnectorID: clickup.ConnectorID, ConnectionName: clickupConnection.Name, TriggerName: "taskEvent",
			BindingName: bindingName, Configuration: json.RawMessage(bindingConfiguration),
		})
	}
	return configuration
}

// recordingInboxes stands in for durable project inboxes, recording each inbox key and every prepared event.
type recordingInboxes struct {
	mu       sync.Mutex
	keys     []projectconfig.TriggerInboxKey
	prepared []sdkgo.TriggerEvent[clickup.TaskEvent]
}

func (inboxes *recordingInboxes) wrap(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[clickup.TaskEvent],
) (sdkgo.TriggerTarget[clickup.TaskEvent], error) {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	inboxes.keys = append(inboxes.keys, key)
	return recordingTarget{inboxes: inboxes, target: target}, nil
}

func (inboxes *recordingInboxes) preparedEvents() []sdkgo.TriggerEvent[clickup.TaskEvent] {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	return slices.Clone(inboxes.prepared)
}

// recordingTarget records an event when the endpoint prepares it and passes deliveries to the application target.
type recordingTarget struct {
	inboxes *recordingInboxes
	target  sdkgo.TriggerTarget[clickup.TaskEvent]
}

func (target recordingTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[clickup.TaskEvent]) error {
	target.inboxes.mu.Lock()
	defer target.inboxes.mu.Unlock()
	target.inboxes.prepared = append(target.inboxes.prepared, event)
	return nil
}

func (target recordingTarget) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[clickup.TaskEvent]) error {
	return target.target.HandleTrigger(ctx, event)
}

// receivedEvents collects the events a binding's target received.
type receivedEvents struct {
	events chan sdkgo.TriggerEvent[clickup.TaskEvent]
}

func (received *receivedEvents) target() sdkgo.TriggerTarget[clickup.TaskEvent] {
	return sdkgo.TriggerTargetFunc[clickup.TaskEvent](func(_ context.Context, event sdkgo.TriggerEvent[clickup.TaskEvent]) error {
		received.events <- event
		return nil
	})
}

func (received *receivedEvents) next(t *testing.T) sdkgo.TriggerEvent[clickup.TaskEvent] {
	t.Helper()
	select {
	case event := <-received.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the binding received no event")
		return sdkgo.TriggerEvent[clickup.TaskEvent]{}
	}
}

func (received *receivedEvents) requireNoMore(t *testing.T) {
	t.Helper()
	select {
	case event := <-received.events:
		t.Fatalf("unexpected event %s", event.ID)
	case <-time.After(100 * time.Millisecond):
	}
}

// runTaskEventBinding runs one binding whose target records events, and returns the connection's handler.
func runTaskEventBinding(
	t *testing.T, credentials sdkgo.CredentialProvider[clickup.Credentials], configuration clickup.TaskEventTriggerConfiguration,
) (http.Handler, *receivedEvents) {
	t.Helper()
	client, err := clickup.New(clickup.Config{}, credentials)
	require.NoError(t, err)
	connection, err := clickup.NewConnection(client, clickupConnection)
	require.NoError(t, err)
	received := &receivedEvents{events: make(chan sdkgo.TriggerEvent[clickup.TaskEvent], 16)}
	runner := clickup.NewTaskEventTrigger(clickup.TaskEventTriggerConfig{
		Connection: connection, ConnectionName: clickupConnection.Name, BindingName: "test-binding", Configuration: configuration,
		Target: received.target(),
	})
	handler, err := connection.TaskEventWebhookHandler()
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	var finished sync.WaitGroup
	finished.Add(1)
	go func() {
		defer finished.Done()
		_ = runner.Run(ctx) // Run returns the cancellation error when the test ends.
	}()
	t.Cleanup(func() {
		cancel()
		finished.Wait()
	})
	return handler, received
}

// waitForBindingToReceive probes until the endpoint stops answering 503, then drains an accepted probe.
func waitForBindingToReceive(t *testing.T, handler http.Handler, received *receivedEvents, configuration clickup.TaskEventTriggerConfiguration) {
	t.Helper()
	probe := statusUpdatedBody("probe1", "1", "to do", "to do")
	require.Eventually(t, func() bool {
		return serveTaskEvent(handler, http.MethodPost, probe, signTaskEvent(probe, testWebhookSecret)) == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)
	if len(configuration.Events) == 0 || slices.Contains(configuration.Events, clickup.EventTaskStatusUpdated) {
		require.Equal(t, testWebhookID+":taskStatusUpdated:1", received.next(t).ID)
	}
}

func serveTaskEvent(handler http.Handler, method string, body []byte, signature string) int {
	request := httptest.NewRequest(method, "/webhooks/clickup", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		request.Header.Set("X-Signature", signature)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

// statusUpdatedBody is ClickUp's documented taskStatusUpdated payload for one status change.
func statusUpdatedBody(taskID string, historyItemID string, before string, after string) []byte {
	return []byte(`{"event":"taskStatusUpdated","history_items":[{"id":"` + historyItemID + `","type":1,"date":"1642734631523","field":"status",` +
		`"parent_id":"162641062","data":{"status_type":"custom"},"source":null,"user":{"id":183,"username":"John","email":"john@company.example.com"},` +
		`"before":{"status":"` + before + `","color":"#d3d3d3","type":"open","orderindex":0},"after":{"status":"` + after + `","color":"#f9d900","type":"custom","orderindex":1}}],` +
		`"task_id":"` + taskID + `","webhook_id":"` + testWebhookID + `"}`)
}

func signTaskEvent(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body) // A hash write never fails.
	return hex.EncodeToString(mac.Sum(nil))
}
