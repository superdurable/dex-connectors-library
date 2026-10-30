// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var webhookNow = time.Date(2026, time.October, 4, 12, 0, 0, 0, time.UTC)

// webhookFixture is one connection whose issueEventReceived binding runs until the test ends.
type webhookFixture struct {
	handler http.Handler
	events  chan sdkgo.TriggerEvent[linear.IssueEvent]
}

func newWebhookFixture(t *testing.T, signingSecret string, configuration linear.IssueEventReceivedTriggerConfiguration) *webhookFixture {
	t.Helper()
	credentials := sdkgo.StaticCredentialProvider[linear.Credentials]{linearConnection: {
		AuthMethodID: linear.PersonalAPIKeyAuthMethodID, APIKey: sdkgo.NewSecretString(testAPIKey), WebhookSigningSecret: sdkgo.NewSecretString(signingSecret),
	}}
	client, err := linear.New(linear.Config{WebhookMaxBodyBytes: 8192}, credentials, linear.WithClock(func() time.Time { return webhookNow }))
	require.NoError(t, err)
	connection, err := linear.NewConnection(client, linearConnection)
	require.NoError(t, err)
	handler, err := connection.IssueEventReceivedWebhookHandler()
	require.NoError(t, err)
	fixture := &webhookFixture{handler: handler, events: make(chan sdkgo.TriggerEvent[linear.IssueEvent], 16)}
	runner := linear.NewIssueEventReceivedTrigger(linear.IssueEventReceivedTriggerConfig{
		Connection: connection, ConnectionName: linearConnection.Name, BindingName: "issues", Configuration: configuration,
		Target: sdkgo.TriggerTargetFunc[linear.IssueEvent](func(_ context.Context, event sdkgo.TriggerEvent[linear.IssueEvent]) error {
			fixture.events <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	runFinished := make(chan error, 1)
	go func() { runFinished <- runner.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.ErrorIs(t, <-runFinished, context.Canceled)
	})
	readiness := handler.(interface{ RunningSourceCount() int })
	require.Eventually(t, func() bool { return readiness.RunningSourceCount() == 1 }, 5*time.Second, time.Millisecond)
	return fixture
}

// deliver posts body with a Linear-Signature under signingSecret; a blank secret sends no header.
func (fixture *webhookFixture) deliver(t *testing.T, body string, signingSecret string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	if signingSecret != "" {
		request.Header.Set("Linear-Signature", linearSignature(signingSecret, body))
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.NotContains(t, response.Body.String(), testSigningSecret)
	return response.Code
}

func (fixture *webhookFixture) receiveEvent(t *testing.T) sdkgo.TriggerEvent[linear.IssueEvent] {
	t.Helper()
	select {
	case event := <-fixture.events:
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("the verified event was not delivered")
		return sdkgo.TriggerEvent[linear.IssueEvent]{}
	}
}

func (fixture *webhookFixture) requireNoEvent(t *testing.T) {
	t.Helper()
	select {
	case event := <-fixture.events:
		t.Fatalf("unexpected event %s", event.ID)
	case <-time.After(50 * time.Millisecond): // Delivery is asynchronous after the 200.
	}
}

// linearSignature signs as Linear documents: hex HMAC-SHA256 of the raw body.
func linearSignature(signingSecret string, body string) string {
	mac := hmac.New(sha256.New, []byte(signingSecret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

// issueWebhookBody is a Linear Issue webhook sent at sentAt for an issue last updated at updatedAt.
func issueWebhookBody(entityType string, action string, teamID string, updatedAt string, sentAt time.Time) string {
	return fmt.Sprintf(`{"action":%q,"type":%q,"createdAt":"2026-10-04T11:59:59.000Z","organizationId":"d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f7a",`+
		`"webhookId":"e5f6a7b8-c9d0-4e1f-8a2b-3c4d5e6f7a8b","webhookTimestamp":%d,"url":"https://linear.app/acme/issue/ENG-7",`+
		`"actor":{"id":%q,"name":"Alice Nguyen","email":"alice@example.com","type":"user"},`+
		`"updatedFrom":{"stateId":%q,"updatedAt":"2026-10-04T11:00:00.000Z","description":"SENTINEL old description"},`+
		`"data":{"id":%q,"identifier":"ENG-7","number":7,"title":"Fire panel wiring","url":"https://linear.app/acme/issue/ENG-7","priority":2,`+
		`"priorityLabel":"High","description":"SENTINEL description","labelIds":[%q],"teamId":%q,"team":{"id":%q,"key":"ENG","name":"Engineering"},`+
		`"stateId":%q,"state":{"id":%q,"name":"In Progress","type":"started","color":"#f2c94c"},"assigneeId":%q,`+
		`"assignee":{"id":%q,"name":"Alice Nguyen","email":"alice@example.com"},"projectId":null,"cycleId":null,"parentId":null,`+
		`"dueDate":"2026-02-18","createdAt":"2026-10-01T09:00:00.000Z","updatedAt":%q,"archivedAt":null,"trashed":null}}`,
		action, entityType, sentAt.UnixMilli(), testUserID, testStateID, testIssueID, testLabelID, teamID, teamID,
		testOtherStateID, testOtherStateID, testUserID, testUserID, updatedAt)
}

func TestIssueWebhookVerifiesTheSignatureAndDeliversTheIssueWithAStableID(t *testing.T) {
	fixture := newWebhookFixture(t, testSigningSecret, linear.IssueEventReceivedTriggerConfiguration{})
	body := issueWebhookBody("Issue", linear.IssueEventActionUpdate, testTeamID, "2026-10-04T11:59:58.123Z", webhookNow.Add(-2*time.Second))

	require.Equal(t, http.StatusOK, fixture.deliver(t, body, testSigningSecret))
	event := fixture.receiveEvent(t)
	updatedAt := time.Date(2026, time.October, 4, 11, 59, 58, 123000000, time.UTC)
	require.Equal(t, fmt.Sprintf("update:%s:%d", testIssueID, updatedAt.UnixMilli()), event.ID)
	require.Equal(t, time.Date(2026, time.October, 4, 11, 59, 59, 0, time.UTC), event.OccurredAt)
	payload := event.Payload
	require.Equal(t, linear.IssueEventActionUpdate, payload.Action)
	require.Equal(t, &linear.IssueEventActor{ID: testUserID, Name: "Alice Nguyen", Type: "user"}, payload.Actor)
	require.Equal(t, "ENG-7", payload.Issue.Identifier)
	require.Equal(t, 7, payload.Issue.Number)
	require.Equal(t, linear.TeamReference{ID: testTeamID, Key: "ENG", Name: "Engineering"}, payload.Issue.Team)
	require.Equal(t, linear.WorkflowStateReference{ID: testOtherStateID, Name: "In Progress", Type: linear.WorkflowStateTypeStarted}, payload.Issue.State)
	require.Equal(t, testUserID, payload.Issue.AssigneeID)
	require.Equal(t, updatedAt, payload.Issue.UpdatedAt)
	require.Equal(t, []string{"description", "stateId", "updatedAt"}, payload.UpdatedFields)
	require.Equal(t, testStateID, payload.PreviousStateID)
	require.NotContains(t, fmt.Sprintf("%+v", payload), "SENTINEL", "neither description nor previous values are carried")
	require.NotContains(t, fmt.Sprintf("%+v", payload), "alice@example.com", "actor and assignee emails are not carried")

	// Linear signs each retry with a new webhookTimestamp; the event ID stays the same.
	retry := issueWebhookBody("Issue", linear.IssueEventActionUpdate, testTeamID, "2026-10-04T11:59:58.123Z", webhookNow)
	require.Equal(t, http.StatusOK, fixture.deliver(t, retry, testSigningSecret))
	require.Equal(t, event.ID, fixture.receiveEvent(t).ID)
}

func TestIssueWebhookRejectsForgedStaleAndUnsignedDeliveries(t *testing.T) {
	fixture := newWebhookFixture(t, testSigningSecret, linear.IssueEventReceivedTriggerConfiguration{})
	fresh := issueWebhookBody("Issue", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, fresh, "not-the-signing-secret"), "wrong secret")
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, fresh, ""), "no signature")
	stale := issueWebhookBody("Issue", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow.Add(-61*time.Second))
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, stale, testSigningSecret), "a replay older than the tolerance")
	future := issueWebhookBody("Issue", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow.Add(61*time.Second))
	require.Equal(t, http.StatusBadRequest, fixture.deliver(t, future, testSigningSecret))

	request := httptest.NewRequest(http.MethodPost, "/webhooks/linear", strings.NewReader(strings.Replace(fresh, "Fire panel", "Forged", 1)))
	request.Header.Set("Linear-Signature", linearSignature(testSigningSecret, fresh))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code, "a body changed after signing")
	fixture.requireNoEvent(t)
}

func TestIssueWebhookAcknowledgesOtherEntitiesAndAppliesTheBindingFilter(t *testing.T) {
	otherTeamID := "f6a7b8c9-d0e1-4f2a-9b3c-4d5e6f7a8b9c"
	fixture := newWebhookFixture(t, testSigningSecret, linear.IssueEventReceivedTriggerConfiguration{
		Actions: []string{linear.IssueEventActionCreate}, TeamID: testTeamID,
	})
	comment := issueWebhookBody("Comment", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusOK, fixture.deliver(t, comment, testSigningSecret))
	update := issueWebhookBody("Issue", linear.IssueEventActionUpdate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusOK, fixture.deliver(t, update, testSigningSecret))
	otherTeam := issueWebhookBody("Issue", linear.IssueEventActionCreate, otherTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusOK, fixture.deliver(t, otherTeam, testSigningSecret))
	fixture.requireNoEvent(t)
	created := issueWebhookBody("Issue", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusOK, fixture.deliver(t, created, testSigningSecret))
	require.Equal(t, linear.IssueEventActionCreate, fixture.receiveEvent(t).Payload.Action)
}

func TestIssueWebhookAnswers503WithoutASigningSecretAnd413ForAnOversizedBody(t *testing.T) {
	unconfigured := newWebhookFixture(t, "", linear.IssueEventReceivedTriggerConfiguration{})
	body := issueWebhookBody("Issue", linear.IssueEventActionCreate, testTeamID, "2026-10-04T11:59:58.000Z", webhookNow)
	require.Equal(t, http.StatusServiceUnavailable, unconfigured.deliver(t, body, testSigningSecret), "Linear retries until the secret is saved")

	configured := newWebhookFixture(t, testSigningSecret, linear.IssueEventReceivedTriggerConfiguration{})
	oversized := strings.Replace(body, "Fire panel wiring", strings.Repeat("x", 9000), 1)
	require.Equal(t, http.StatusRequestEntityTooLarge, configured.deliver(t, oversized, testSigningSecret))
	malformed := `{"type":"Issue","action":"create","webhookTimestamp":` + fmt.Sprint(webhookNow.UnixMilli()) + `,"data":{"id":"x"}}`
	require.Equal(t, http.StatusBadRequest, configured.deliver(t, malformed, testSigningSecret), "a verified but malformed body")
}

func TestIssueEventReceivedTriggerConfigurationValidation(t *testing.T) {
	require.NoError(t, linear.IssueEventReceivedTriggerConfiguration{}.Validate())
	require.NoError(t, linear.IssueEventReceivedTriggerConfiguration{Actions: []string{"create", "remove"}, TeamID: testTeamID}.Validate())
	require.Error(t, linear.IssueEventReceivedTriggerConfiguration{Actions: []string{"create", "create"}}.Validate())
	require.Error(t, linear.IssueEventReceivedTriggerConfiguration{Actions: []string{"archive"}}.Validate())
	require.Error(t, linear.IssueEventReceivedTriggerConfiguration{TeamID: "ENG"}.Validate())
}
