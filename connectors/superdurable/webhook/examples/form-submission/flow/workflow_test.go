// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package formsubmission

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/superdurable/webhook"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func submissionEvent(eventID string, payload webhook.WebhookRequestEvent) sdkgo.TriggerEvent[webhook.WebhookRequestEvent] {
	return sdkgo.TriggerEvent[webhook.WebhookRequestEvent]{ID: eventID, OccurredAt: payload.ReceivedAt, Payload: payload}
}

func TestRedeliveriesResolveOneFlowAndCarryTheSubmission(t *testing.T) {
	receivedAt := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	event := submissionEvent("evt_1", webhook.WebhookRequestEvent{
		ContentType: webhook.ContentTypeJSON, JSONBody: json.RawMessage(`{"email":"ada@example.com"}`), ReceivedAt: receivedAt,
	})
	redelivery := event
	redelivery.Payload.ReceivedAt = receivedAt.Add(time.Minute)
	require.Equal(t, "webhook-form-submission-evt_1", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(redelivery))
	require.Equal(t, Submission{
		EventID: "evt_1", ContentType: webhook.ContentTypeJSON, ReceivedAt: receivedAt, JSONBody: json.RawMessage(`{"email":"ada@example.com"}`),
	}, MapToFlowInput(event))
}

func TestAcceptSubmissionRequiresFieldsOrAJSONObject(t *testing.T) {
	for _, test := range []struct {
		name       string
		payload    webhook.WebhookRequestEvent
		isAccepted bool
	}{
		{name: "JSON object", payload: webhook.WebhookRequestEvent{JSONBody: json.RawMessage(`{"email":"ada@example.com"}`)}, isAccepted: true},
		{name: "form fields", payload: webhook.WebhookRequestEvent{FormBody: map[string][]string{"email": {"ada@example.com"}}}, isAccepted: true},
		{name: "empty JSON object", payload: webhook.WebhookRequestEvent{JSONBody: json.RawMessage(`{}`)}},
		{name: "JSON array", payload: webhook.WebhookRequestEvent{JSONBody: json.RawMessage(`[1,2]`)}},
		{name: "empty form", payload: webhook.WebhookRequestEvent{FormBody: map[string][]string{}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.isAccepted, AcceptSubmission(submissionEvent("evt", test.payload)))
		})
	}
}

func TestForwardedEventCarriesTheSubmissionAndItsID(t *testing.T) {
	receivedAt := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	input := MapToForwardedEvent(Submission{
		EventID: "evt_1", ContentType: webhook.ContentTypeForm, ReceivedAt: receivedAt, FormBody: map[string][]string{"email": {"ada@example.com"}},
	})
	require.JSONEq(t, `{"type":"form.submitted","submissionId":"evt_1","receivedAt":"2026-09-30T12:00:00Z","formBody":{"email":["ada@example.com"]}}`,
		string(input.Payload))
}

func TestFlowDeclaresItsSubmissionBinding(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, SubmissionTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, webhook.RequestReceivedTriggerDefinition, bindings[0].Definition)
	require.Equal(t, FlowType, (&Flow{}).GetFlowType())
}
