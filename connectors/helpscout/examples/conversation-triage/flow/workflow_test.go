// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package conversationtriage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func conversationEvent(eventID string, webhookEvent string, status helpscout.ConversationStatus) sdkgo.TriggerEvent[helpscout.ConversationEvent] {
	receivedAt := time.Date(2026, time.September, 30, 11, 59, 0, 0, time.UTC)
	return sdkgo.TriggerEvent[helpscout.ConversationEvent]{ID: eventID, OccurredAt: receivedAt, Payload: helpscout.ConversationEvent{
		Event: webhookEvent, Conversation: helpscout.Conversation{ID: 501, MailboxID: 123, Status: status, Subject: "Double charge"},
	}}
}

func TestRedeliveriesResolveOneFlowAndCarryTheConversation(t *testing.T) {
	event := conversationEvent("convo.created:501:0f1e2d3c4b5a69788796a5b4c3d2e1f0", helpscout.WebhookEventConversationCreated, helpscout.ConversationStatusActive)
	redelivery := event
	redelivery.OccurredAt = event.OccurredAt.Add(time.Minute)
	require.Equal(t, "helpscout-convo.created-501-0f1e2d3c4b5a69788796a5b4c3d2e1f0", ResolveFlowID(event))
	require.Equal(t, ResolveFlowID(event), ResolveFlowID(redelivery))
	request := MapToFlowInput(event)
	require.Equal(t, TriageRequest{EventID: event.ID, ConversationID: 501, MailboxID: 123, Subject: "Double charge", ReceivedAt: event.OccurredAt}, request)
	require.Equal(t, helpscout.GetConversationInput{ConversationID: 501, ThreadLimit: NewestThreadLimit}, MapToGetConversationInput(request))
}

func TestAcceptNewConversationAdmitsOnlyActiveNewConversations(t *testing.T) {
	require.True(t, AcceptNewConversation(conversationEvent("a", helpscout.WebhookEventConversationCreated, helpscout.ConversationStatusActive)))
	require.False(t, AcceptNewConversation(conversationEvent("b", helpscout.WebhookEventConversationTagsUpdated, helpscout.ConversationStatusActive)))
	require.False(t, AcceptNewConversation(conversationEvent("c", helpscout.WebhookEventConversationCreated, helpscout.ConversationStatusSpam)))
	require.False(t, AcceptNewConversation(conversationEvent("d", helpscout.WebhookEventConversationCreated, helpscout.ConversationStatusClosed)))
}

func TestMappingsReadTheCustomerAndWriteOnlyAnInternalNoteAndTags(t *testing.T) {
	triageContext := TriageContext{ConversationID: 501, MailboxID: 123, CustomerEmail: "jane@acme.example.com"}
	require.Equal(t, helpscout.FindCustomerByEmailInput{Email: "jane@acme.example.com"}, MapToFindCustomerByEmailInput(triageContext))
	require.Equal(t, helpscout.SearchConversationsInput{MailboxID: 123, Status: helpscout.ConversationStatusActive, CustomerEmail: "jane@acme.example.com"},
		MapToSearchConversationsInput(triageContext))
	require.Equal(t, helpscout.ReplyToConversationInput{ConversationID: 501, Text: "note", IsInternalNote: true},
		MapToReplyToConversationInput(TriageNote{ConversationID: 501, Text: "note"}), "the Flow never emails the customer")
	require.Equal(t, helpscout.UpdateConversationInput{ConversationID: 501, AddTags: []string{TriagedTag}}, MapToUpdateConversationInput(triageContext))
	triageContext.IsRepeatContact = true
	require.Equal(t, []string{TriagedTag, RepeatContactTag}, MapToUpdateConversationInput(triageContext).AddTags)
}

func TestBuildTriageNoteNamesProfilesAndOtherConversations(t *testing.T) {
	require.Equal(t, "Dex triage: 1 Help Scout customer profile matches jane@acme.example.com; the customer has no other active conversation in this inbox.",
		BuildTriageNote("jane@acme.example.com", []int64{1001}, nil))
	require.Equal(t, "Dex triage: 2 Help Scout customer profiles match jane@acme.example.com; the customer has 1 other active conversation in this inbox: 502.",
		BuildTriageNote("jane@acme.example.com", []int64{1001, 1002}, []int64{502}))
	require.Equal(t, "Dex triage: 0 Help Scout customer profiles match jane@acme.example.com; the customer has no other active conversation in this inbox.",
		BuildTriageNote("jane@acme.example.com", nil, nil))
	many := make([]int64, 12)
	for index := range many {
		many[index] = int64(600 + index)
	}
	require.Contains(t, BuildTriageNote("jane@acme.example.com", nil, many), "12 other active conversations in this inbox: 600, 601")
	require.NotContains(t, BuildTriageNote("jane@acme.example.com", nil, many), "610", "at most ten IDs are listed")
}

func TestFlowDeclaresItsBindingWithTheMailboxPicker(t *testing.T) {
	bindings := (&Flow{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, NewConversationTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Equal(t, helpscout.ConversationEventTriggerDefinition, bindings[0].Definition)
	require.NotNil(t, bindings[0].ConfigurationUI)
	units := bindings[0].ConfigurationUI.Units
	require.Len(t, units, 1)
	require.Equal(t, helpscout.UIUnitMailboxPicker, units[0].UnitID)
	require.False(t, units[0].Required, "blank triages every inbox")
	require.Contains(t, units[0].Description, "Leave it empty")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: helpscout.UIMailboxPickerPortMailboxID, JSONPointer: "/mailboxId"}}, units[0].Bindings)
	require.Equal(t, FlowType, (&Flow{}).GetFlowType())
}
