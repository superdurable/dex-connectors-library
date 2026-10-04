// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
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
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestConversationEventEndpointVerifiesDecodesAndFiltersNotifications(t *testing.T) {
	configuration := intercom.ConversationEventTriggerConfiguration{
		Topics: []string{intercom.TopicConversationUserCreated, intercom.TopicConversationUserReplied},
	}
	endpoint, received := runConversationEventBinding(t, testCredentialProvider(), configuration)
	waitForBindingToReceive(t, endpoint, received, configuration)
	created := notificationBody("notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3", intercom.TopicConversationUserCreated, conversationJSON(t, testConversation, "open", nil))

	require.Equal(t, http.StatusOK, serveNotification(endpoint, http.MethodHead, nil, ""), "Intercom validates the URL with HEAD")
	require.Equal(t, http.StatusMethodNotAllowed, serveNotification(endpoint, http.MethodGet, nil, ""))
	upperHexSignature := "sha1=" + strings.ToUpper(strings.TrimPrefix(signNotification(created, testClientSecret), "sha1="))
	require.Equal(t, http.StatusOK, serveNotification(endpoint, http.MethodPost, created, upperHexSignature))
	event := received.next(t)
	require.Equal(t, "notif_ccd8a4d0-f965-11e3-a367-c779cae3e1b3", event.ID)
	require.Equal(t, time.Unix(1767229300, 0).UTC(), event.OccurredAt)
	require.Equal(t, intercom.ConversationEvent{
		NotificationID: event.ID, Topic: intercom.TopicConversationUserCreated, WorkspaceID: "ecahpwf5", NotifiedAt: event.OccurredAt,
		Conversation: event.Payload.Conversation,
	}, event.Payload)
	require.Equal(t, testConversation, event.Payload.Conversation.ID)
	require.Equal(t, "jane@acme.example.com", event.Payload.Conversation.Source.Author.Email)
	require.Empty(t, event.Payload.Conversation.Source.Body, "the event carries no message text")

	for name, request := range map[string]struct {
		body      []byte
		signature string
		status    int
	}{
		"wrong secret":            {body: created, signature: signNotification(created, "another-secret"), status: http.StatusBadRequest},
		"missing signature":       {body: created, status: http.StatusBadRequest},
		"sha256 signature":        {body: created, signature: "sha256=" + strings.Repeat("a", 64), status: http.StatusBadRequest},
		"tampered body":           {body: []byte(strings.Replace(string(created), "jane@", "eve@", 1)), signature: signNotification(created, testClientSecret), status: http.StatusBadRequest},
		"malformed JSON":          {body: []byte(`{"type":`), signature: signNotification([]byte(`{"type":`), testClientSecret), status: http.StatusBadRequest},
		"not a notification":      {body: []byte(`{"type":"conversation","id":"1"}`), signature: signNotification([]byte(`{"type":"conversation","id":"1"}`), testClientSecret), status: http.StatusBadRequest},
		"ping":                    {body: notificationBody("notif_ping", "ping", `{"type":"ping","message":"something"}`), status: http.StatusOK},
		"contact topic":           {body: notificationBody("notif_contact", "contact.user.created", `{"type":"contact","id":"5ba6"}`), status: http.StatusOK},
		"topic the binding skips": {body: notificationBody("notif_closed", intercom.TopicConversationAdminClosed, conversationJSON(t, testConversation, "closed", nil)), status: http.StatusOK},
	} {
		signature := request.signature
		if signature == "" && request.status == http.StatusOK {
			signature = signNotification(request.body, testClientSecret)
		}
		require.Equal(t, request.status, serveNotification(endpoint, http.MethodPost, request.body, signature), name)
	}
	received.requireNoMore(t)
}

func TestConversationEventEndpointAsksIntercomToRetryUntilItCanVerifyAndRecord(t *testing.T) {
	created := notificationBody("notif_retry", intercom.TopicConversationUserCreated, conversationJSON(t, testConversation, "open", nil))
	withoutSecret, err := intercom.New(intercom.Config{Region: intercom.RegionEu}, sdkgo.StaticCredentialProvider[intercom.Credentials]{
		intercomConnection: {AccessToken: sdkgo.NewSecretString(testAccessToken)},
	})
	require.NoError(t, err)
	connectionWithoutSecret, err := intercom.NewConnection(withoutSecret, intercomConnection)
	require.NoError(t, err)
	inboxes := &recordingInboxes{}
	runner, err := intercom.NewConversationEventEndpointRunnerForTest(connectionWithoutSecret, projectConfiguration(map[string]string{"test-binding": `{}`}),
		[]intercom.ProjectConversationEventTriggerRoute{{
			BindingName: "test-binding",
			Target: sdkgo.TriggerTargetFunc[intercom.ConversationEvent](func(context.Context, sdkgo.TriggerEvent[intercom.ConversationEvent]) error {
				t.Error("no notification may be delivered without a client secret")
				return nil
			}),
		}}, inboxes.wrap)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(ctx) }()
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, http.StatusOK, serveNotification(runner, http.MethodHead, nil, ""))
	require.Equal(t, http.StatusServiceUnavailable, serveNotification(runner, http.MethodPost, created, signNotification(created, testClientSecret)),
		"a blank client_secret answers 503 so Intercom retries")
	require.Empty(t, inboxes.preparedEvents(), "an unverified notification is not recorded")
	cancel()
	require.ErrorIs(t, <-runResult, context.Canceled)

	client, err := intercom.New(intercom.Config{}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, intercomConnection)
	require.NoError(t, err)
	idle, err := connection.ConversationEventWebhookHandler()
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, serveNotification(idle, http.MethodHead, nil, ""), "HEAD succeeds before any binding runs")
	require.Equal(t, http.StatusServiceUnavailable, serveNotification(idle, http.MethodPost, created, signNotification(created, testClientSecret)),
		"no running binding answers 503 so Intercom retries")
}

func TestConversationEventEndpointRejectsAnOversizedBody(t *testing.T) {
	client, err := intercom.New(intercom.Config{WebhookMaxBodyBytes: 64}, testCredentialProvider())
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, intercomConnection)
	require.NoError(t, err)
	handler, err := connection.ConversationEventWebhookHandler()
	require.NoError(t, err)
	body := notificationBody("notif_large", intercom.TopicConversationUserCreated, conversationJSON(t, testConversation, "open", nil))
	require.Equal(t, http.StatusRequestEntityTooLarge, serveNotification(handler, http.MethodPost, body, signNotification(body, testClientSecret)))
}

func TestConversationEventTriggerConfigurationAcceptsOnlySupportedTopics(t *testing.T) {
	require.NoError(t, intercom.ConversationEventTriggerConfiguration{}.Validate())
	require.NoError(t, intercom.ConversationEventTriggerConfiguration{Topics: intercom.ConversationEventTopics()}.Validate())
	for name, topics := range map[string][]string{
		"ping":           {"ping"},
		"ticket topic":   {"ticket.created"},
		"deleted topic":  {"conversation.deleted"},
		"duplicate":      {intercom.TopicConversationUserCreated, intercom.TopicConversationUserCreated},
		"different case": {"Conversation.User.Created"},
	} {
		require.Error(t, intercom.ConversationEventTriggerConfiguration{Topics: topics}.Validate(), name)
	}
	topics := intercom.ConversationEventTopics()
	topics[0] = "mutated"
	require.Equal(t, intercom.TopicConversationUserCreated, intercom.ConversationEventTopics()[0], "callers get a copy")
}

// TestStudioTopicPickerListsTheSupportedTopics keeps ui/src/topics.ts equal to ConversationEventTopics.
func TestStudioTopicPickerListsTheSupportedTopics(t *testing.T) {
	contents, err := os.ReadFile("ui/src/topics.ts")
	require.NoError(t, err)
	var uiTopics []string
	for _, match := range regexp.MustCompile(`\{topic: "([^"]+)"`).FindAllStringSubmatch(string(contents), -1) {
		uiTopics = append(uiTopics, match[1])
	}
	require.Equal(t, intercom.ConversationEventTopics(), uiTopics)
}

func TestEndpointRunnerRecordsBeforeAcknowledging(t *testing.T) {
	inboxes := &recordingInboxes{}
	received := &receivedEvents{events: make(chan sdkgo.TriggerEvent[intercom.ConversationEvent], 4)}
	runner, err := intercom.NewConversationEventEndpointRunnerForTest(newTestConnection(t),
		projectConfiguration(map[string]string{"test-binding": `{"topics":["` + intercom.TopicConversationUserCreated + `"]}`}),
		[]intercom.ProjectConversationEventTriggerRoute{{
			BindingName: "test-binding",
			Target: sdkgo.TriggerTargetFunc[intercom.ConversationEvent](func(_ context.Context, event sdkgo.TriggerEvent[intercom.ConversationEvent]) error {
				received.events <- event
				return nil
			}),
		}}, inboxes.wrap)
	require.NoError(t, err)
	require.Equal(t, []projectconfig.TriggerInboxKey{{
		ConnectorID: intercom.ConnectorID, ConnectionName: intercomConnection.Name, TriggerName: "conversationEvent", BindingName: "test-binding",
	}}, inboxes.keys, "every route's target is wrapped in its binding's durable inbox")
	ctx, cancel := context.WithCancel(context.Background())
	runResult := make(chan error, 1)
	go func() { runResult <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runResult, context.Canceled)
	})
	require.Eventually(t, func() bool { return runner.RunningSourceCount() == 1 }, 5*time.Second, 10*time.Millisecond)

	created := notificationBody("notif_recorded", intercom.TopicConversationUserCreated, conversationJSON(t, testConversation, "open", nil))
	require.Equal(t, http.StatusOK, serveNotification(runner, http.MethodPost, created, signNotification(created, testClientSecret)))
	prepared := inboxes.preparedEvents()
	require.Len(t, prepared, 1, "the 200 came after the notification was recorded")
	require.Equal(t, "notif_recorded", prepared[0].ID)
	stored, err := json.Marshal(prepared[0])
	require.NoError(t, err)
	require.NotContains(t, string(stored), testClientSecret)
	require.NotContains(t, string(stored), testAccessToken)
	require.Equal(t, "notif_recorded", received.next(t).ID)
}

func TestEndpointRunnerRejectsMissingOrInvalidBindings(t *testing.T) {
	target := sdkgo.TriggerTargetFunc[intercom.ConversationEvent](func(context.Context, sdkgo.TriggerEvent[intercom.ConversationEvent]) error { return nil })
	connection := newTestConnection(t)
	configuration := projectConfiguration(map[string]string{"test-binding": `{"topics":["` + intercom.TopicConversationUserCreated + `"]}`})
	inboxes := &recordingInboxes{}
	for name, routes := range map[string][]intercom.ProjectConversationEventTriggerRoute{
		"no routes":         nil,
		"unstored binding":  {{BindingName: "another-binding", Target: target}},
		"blank binding":     {{BindingName: " ", Target: target}},
		"nil target":        {{BindingName: "test-binding"}},
		"duplicate binding": {{BindingName: "test-binding", Target: target}, {BindingName: "test-binding", Target: target}},
	} {
		_, err := intercom.NewConversationEventEndpointRunnerForTest(connection, configuration, routes, inboxes.wrap)
		require.Error(t, err, name)
	}
	_, err := intercom.NewConversationEventEndpointRunnerForTest(connection, configuration,
		[]intercom.ProjectConversationEventTriggerRoute{{BindingName: "another-binding", Target: target}}, inboxes.wrap)
	require.ErrorIs(t, err, projectconfig.ErrObjectNotFound)
	invalidTopics := projectConfiguration(map[string]string{"test-binding": `{"topics":["ticket.created"]}`})
	_, err = intercom.NewConversationEventEndpointRunnerForTest(connection, invalidTopics,
		[]intercom.ProjectConversationEventTriggerRoute{{BindingName: "test-binding", Target: target}}, inboxes.wrap)
	require.ErrorContains(t, err, "not a supported conversation topic")
	inboxFailure := errors.New("project trigger inbox is unavailable")
	_, err = intercom.NewConversationEventEndpointRunnerForTest(connection, configuration,
		[]intercom.ProjectConversationEventTriggerRoute{{BindingName: "test-binding", Target: target}},
		func(projectconfig.TriggerInboxKey, sdkgo.TriggerTarget[intercom.ConversationEvent]) (sdkgo.TriggerTarget[intercom.ConversationEvent], error) {
			return nil, inboxFailure
		})
	require.ErrorIs(t, err, inboxFailure)
	_, err = intercom.NewProjectConversationEventEndpointRunner(nil, intercomConnection.Name,
		[]intercom.ProjectConversationEventTriggerRoute{{BindingName: "test-binding", Target: target}})
	require.Error(t, err)
	runner, err := intercom.NewConversationEventEndpointRunnerForTest(connection, configuration,
		[]intercom.ProjectConversationEventTriggerRoute{{BindingName: "test-binding", Target: target}}, inboxes.wrap)
	require.NoError(t, err)
	require.Zero(t, runner.RunningSourceCount())
}

// projectConfiguration is the project configuration Dex Web saves for the connection and its conversationEvent bindings.
func projectConfiguration(bindings map[string]string) projectconfig.Configuration {
	configuration := projectconfig.Configuration{Connections: []projectconfig.ConnectionConfiguration{{
		ConnectorID: intercom.ConnectorID, ConnectionName: intercomConnection.Name,
		ModulePath: "github.com/superdurable/dex-connectors-library/connectors/intercom", Provider: "intercom",
		Configuration: json.RawMessage(`{"region":"eu"}`),
	}}}
	for bindingName, bindingConfiguration := range bindings {
		configuration.TriggerBindings = append(configuration.TriggerBindings, projectconfig.TriggerConfiguration{
			ConnectorID: intercom.ConnectorID, ConnectionName: intercomConnection.Name, TriggerName: "conversationEvent",
			BindingName: bindingName, Configuration: json.RawMessage(bindingConfiguration),
		})
	}
	return configuration
}

// recordingInboxes stands in for durable project inboxes, recording each inbox key and every prepared event.
type recordingInboxes struct {
	mu       sync.Mutex
	keys     []projectconfig.TriggerInboxKey
	prepared []sdkgo.TriggerEvent[intercom.ConversationEvent]
}

func (inboxes *recordingInboxes) wrap(
	key projectconfig.TriggerInboxKey, target sdkgo.TriggerTarget[intercom.ConversationEvent],
) (sdkgo.TriggerTarget[intercom.ConversationEvent], error) {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	inboxes.keys = append(inboxes.keys, key)
	return recordingTarget{inboxes: inboxes, target: target}, nil
}

func (inboxes *recordingInboxes) preparedEvents() []sdkgo.TriggerEvent[intercom.ConversationEvent] {
	inboxes.mu.Lock()
	defer inboxes.mu.Unlock()
	return slices.Clone(inboxes.prepared)
}

// recordingTarget records an event when the endpoint prepares it and passes deliveries to the application target.
type recordingTarget struct {
	inboxes *recordingInboxes
	target  sdkgo.TriggerTarget[intercom.ConversationEvent]
}

func (target recordingTarget) PrepareTrigger(_ context.Context, event sdkgo.TriggerEvent[intercom.ConversationEvent]) error {
	target.inboxes.mu.Lock()
	defer target.inboxes.mu.Unlock()
	target.inboxes.prepared = append(target.inboxes.prepared, event)
	return nil
}

func (target recordingTarget) HandleTrigger(ctx context.Context, event sdkgo.TriggerEvent[intercom.ConversationEvent]) error {
	return target.target.HandleTrigger(ctx, event)
}

// receivedEvents collects the events a binding's target received.
type receivedEvents struct {
	events chan sdkgo.TriggerEvent[intercom.ConversationEvent]
}

func (received *receivedEvents) next(t *testing.T) sdkgo.TriggerEvent[intercom.ConversationEvent] {
	t.Helper()
	select {
	case event := <-received.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the binding received no event")
		return sdkgo.TriggerEvent[intercom.ConversationEvent]{}
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

// runConversationEventBinding runs one binding whose target records events, and returns the connection's handler.
func runConversationEventBinding(
	t *testing.T, credentials sdkgo.CredentialProvider[intercom.Credentials], configuration intercom.ConversationEventTriggerConfiguration,
) (http.Handler, *receivedEvents) {
	t.Helper()
	client, err := intercom.New(intercom.Config{}, credentials)
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, intercomConnection)
	require.NoError(t, err)
	received := &receivedEvents{events: make(chan sdkgo.TriggerEvent[intercom.ConversationEvent], 16)}
	runner := intercom.NewConversationEventTrigger(intercom.ConversationEventTriggerConfig{
		Connection: connection, ConnectionName: intercomConnection.Name, BindingName: "test-binding", Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[intercom.ConversationEvent](func(_ context.Context, event sdkgo.TriggerEvent[intercom.ConversationEvent]) error {
			received.events <- event
			return nil
		}),
	})
	handler, err := connection.ConversationEventWebhookHandler()
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

// waitForBindingToReceive probes with a rating notification, which the endpoint answers 503 until the
// binding runs, and drains the probe when the binding's topics accept it.
func waitForBindingToReceive(t *testing.T, handler http.Handler, received *receivedEvents, configuration intercom.ConversationEventTriggerConfiguration) {
	t.Helper()
	probe := notificationBody("notif_probe", intercom.TopicConversationRatingAdded, conversationJSON(t, testConversation, "open", nil))
	require.Eventually(t, func() bool {
		return serveNotification(handler, http.MethodPost, probe, signNotification(probe, testClientSecret)) == http.StatusOK
	}, 5*time.Second, 10*time.Millisecond)
	if len(configuration.Topics) == 0 || slices.Contains(configuration.Topics, intercom.TopicConversationRatingAdded) {
		require.Equal(t, "notif_probe", received.next(t).ID)
	}
}

func serveNotification(handler http.Handler, method string, body []byte, signature string) int {
	request := httptest.NewRequest(method, "/webhooks/intercom", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	if signature != "" {
		request.Header.Set("X-Hub-Signature", signature)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

func notificationBody(notificationID string, topic string, item string) []byte {
	return []byte(`{"type":"notification_event","app_id":"ecahpwf5","id":"` + notificationID + `","topic":"` + topic +
		`","data":{"type":"notification_event_data","item":` + item + `},"links":{},"delivery_status":"pending","delivery_attempts":1,` +
		`"delivered_at":0,"first_sent_at":1767229301,"created_at":1767229300,"self":null}`)
}

func signNotification(body []byte, secret string) string {
	mac := hmac.New(sha1.New, []byte(secret))
	_, _ = mac.Write(body) // A hash write never fails.
	return "sha1=" + hex.EncodeToString(mac.Sum(nil))
}
