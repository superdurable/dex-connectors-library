// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package answerduplicate

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestFlowIdentitiesMatchTheFlowDefinition(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName), sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]{})
	require.Equal(t, FlowType, dex.GetFinalFlowType(flow))
	require.Equal(t, recordInboundConversationStepType, dex.GetFinalStepType[InboundConversation](recordInboundConversation{}))
	require.Equal(t, chooseCustomerEmailStepType, dex.GetFinalStepType[intercom.GetConversationResult](chooseCustomerEmail{}))
	require.Equal(t, completeWithoutContactStepType, dex.GetFinalStepType[intercom.FindContactByEmailResult](completeWithoutContact{}))
	require.Equal(t, chooseEarlierConversationStepType, dex.GetFinalStepType[intercom.SearchConversationsResult](chooseEarlierConversation{}))
	require.Equal(t, recordDuplicateReplyStepType, dex.GetFinalStepType[intercom.ReplyToConversationResult](recordDuplicateReply{}))
	require.Equal(t, recordReplyNeedsReviewStepType, dex.GetFinalStepType[intercom.ReplyToConversationResult](recordReplyNeedsReview{}))
	require.Equal(t, completeAnsweredDuplicateStepType, dex.GetFinalStepType[intercom.UpdateConversationStateResult](completeAnsweredDuplicate{}))
	wait, err := recordInboundConversation{}.WaitFor(nil, InboundConversation{})
	require.NoError(t, err)
	require.Equal(t, dex.SkipWaitImmediately(), wait)
	require.Equal(t, sdkgo.ConnectorConfigurationRef{
		ConnectorID: "intercom", ConnectionName: ConnectionName, OperationID: "replyToConversation", FlowType: FlowType, StepType: "ReplyToDuplicate",
	}, ReplyConfigurationRef())
}

func TestFlowRegistersOnlyWithItsStaticConnection(t *testing.T) {
	_, err := dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, ConnectionName), sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]{})})
	require.NoError(t, err)
	require.Panics(t, func() {
		_, _ = dex.NewRegistry([]dex.Flow{NewFlow(newUnitTestConnection(t, "another-connection"), sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]{})})
	})
}

func TestTriggerBindingSelectsTopicsWithGuidance(t *testing.T) {
	bindings := NewFlow(newUnitTestConnection(t, ConnectionName), sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]{}).GetConnectorTriggerBindings()
	require.Len(t, bindings, 1)
	require.Equal(t, InboundTriggerBinding, bindings[0].BindingName)
	require.Equal(t, ConnectionName, bindings[0].ConnectionName)
	require.Len(t, bindings[0].ConfigurationUI.Units, 1)
	unit := bindings[0].ConfigurationUI.Units[0]
	require.Equal(t, intercom.UIUnitConversationTopicPicker, unit.UnitID)
	require.Contains(t, unit.Description, "conversation.user.created")
	require.Contains(t, unit.Description, "Configure > Webhooks")
	require.Equal(t, []sdkgo.ConnectorUIBinding{{Port: intercom.UIConversationTopicPickerPortTopics, JSONPointer: "/topics"}}, unit.Bindings)
}

func TestTriggerAdmissionStartsOneFlowPerNewConversation(t *testing.T) {
	notifiedAt := time.Unix(1767229300, 0).UTC()
	event := sdkgo.TriggerEvent[intercom.ConversationEvent]{ID: "notif_1", OccurredAt: notifiedAt, Payload: intercom.ConversationEvent{
		NotificationID: "notif_1", Topic: intercom.TopicConversationUserCreated, WorkspaceID: "ecahpwf5", NotifiedAt: notifiedAt,
		Conversation: intercom.Conversation{ID: "215472658213"},
	}}
	require.True(t, AcceptInboundConversation(event))
	require.Equal(t, "intercom-duplicate-conversation-215472658213", ResolveFlowID(event))
	require.Equal(t, InboundConversation{
		ConversationID: "215472658213", NotificationID: "notif_1", Topic: intercom.TopicConversationUserCreated, WorkspaceID: "ecahpwf5", NotifiedAt: &notifiedAt,
	}, MapToFlowInput(event))
	for _, topic := range []string{intercom.TopicConversationUserReplied, intercom.TopicConversationAdminReplied, intercom.TopicConversationAdminClosed} {
		event.Payload.Topic = topic
		require.False(t, AcceptInboundConversation(event), topic)
	}
}

func TestChooseCustomerSkipsConversationsNoLongerWaitingForAFirstAnswer(t *testing.T) {
	customer := intercom.ConversationAuthor{Type: "user", ID: "5ba6", Email: " jane@acme.example.com "}
	details := func(state intercom.ConversationState, author intercom.ConversationAuthor, parts ...intercom.ConversationPart) intercom.ConversationDetails {
		return intercom.ConversationDetails{Conversation: intercom.Conversation{State: state, Source: intercom.ConversationSource{Author: author}}, LatestParts: parts}
	}
	email, reason := ChooseCustomer(details(intercom.ConversationStateOpen, customer,
		intercom.ConversationPart{PartType: "note", Author: intercom.ConversationAuthor{Type: "admin"}}))
	require.Equal(t, "jane@acme.example.com", email)
	require.Empty(t, reason, "an internal note is not an answer")
	for expectedReason, conversation := range map[string]intercom.ConversationDetails{
		"notOpen":         details(intercom.ConversationStateSnoozed, customer),
		"alreadyAnswered": details(intercom.ConversationStateOpen, customer, intercom.ConversationPart{PartType: "comment", Author: intercom.ConversationAuthor{Type: "admin"}}),
		"noCustomerEmail": details(intercom.ConversationStateOpen, intercom.ConversationAuthor{Type: "admin", Email: "ada@acme.example.com"}),
	} {
		email, reason := ChooseCustomer(conversation)
		require.Empty(t, email)
		require.Equal(t, expectedReason, reason)
	}
	_, reason = ChooseCustomer(details(intercom.ConversationStateOpen, intercom.ConversationAuthor{Type: "lead"}))
	require.Equal(t, "noCustomerEmail", reason)
}

func TestChooseEarlierConversationPicksTheOldestOtherOpenOrSnoozedOne(t *testing.T) {
	inboundCreatedAt := time.Unix(1767229200, 0)
	inbound := InboundConversationState{Input: InboundConversation{ConversationID: "30"}, CreatedAt: inboundCreatedAt}
	conversation := func(id string, state intercom.ConversationState, createdAgo time.Duration) intercom.Conversation {
		return intercom.Conversation{ID: id, State: state, CreatedAt: inboundCreatedAt.Add(-createdAgo)}
	}
	chosen, isFound := ChooseEarlierConversation([]intercom.Conversation{
		conversation("30", intercom.ConversationStateOpen, 0),
		conversation("20", intercom.ConversationStateSnoozed, time.Hour),
		conversation("10", intercom.ConversationStateOpen, 2*time.Hour),
		conversation("5", intercom.ConversationStateClosed, 3*time.Hour),
		conversation("40", intercom.ConversationStateOpen, -time.Minute),
	}, inbound)
	require.True(t, isFound)
	require.Equal(t, "10", chosen.ID)
	_, isFound = ChooseEarlierConversation([]intercom.Conversation{conversation("30", intercom.ConversationStateOpen, 0), conversation("40", intercom.ConversationStateOpen, -time.Minute)}, inbound)
	require.False(t, isFound, "only the inbound conversation and a newer one")
}

func TestMappersPassOnlyTheRecordedConversation(t *testing.T) {
	flow := NewFlow(newUnitTestConnection(t, ConnectionName), sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]{Value: ReplyConfiguration{AdminID: "5017691"}})
	require.Equal(t, intercom.GetConversationInput{ConversationID: "30", LatestPartLimit: 10}, MapToGetConversationInput(ConversationReference{ConversationID: "30"}))
	require.Equal(t, intercom.FindContactByEmailInput{Email: "jane@acme.example.com", ContactLimit: 15}, MapToFindContactByEmailInput(CustomerEmail{Email: "jane@acme.example.com"}))

	var contacts []intercom.Contact
	for index := range 20 {
		contacts = append(contacts, intercom.Contact{ID: fmt.Sprintf("contact%02d", index%17)})
	}
	search := MapToSearchConversationsInput(intercom.FindContactByEmailResult{Value: intercom.FindContactByEmailOutput{Contacts: contacts}})
	require.Len(t, search.ContactIDs, intercom.MaxSearchFilterValues, "contact IDs are deduplicated and capped at Intercom's group limit")
	require.Equal(t, []intercom.ConversationState{intercom.ConversationStateOpen, intercom.ConversationStateSnoozed}, search.States)
	require.Equal(t, 20, search.PageSize)
	_, err := intercom.BuildConversationSearchQuery(search)
	require.NoError(t, err)

	require.Equal(t, intercom.ReplyToConversationInput{
		ConversationID: "30", AdminID: "5017691", MessageType: intercom.ReplyMessageTypeComment, Body: DuplicateReply,
	}, flow.mapToReplyToConversationInput(DuplicateConversation{ConversationID: "30", EarlierConversationID: "10"}))
	require.Equal(t, intercom.UpdateConversationStateInput{ConversationID: "30", AdminID: "5017691", State: intercom.ConversationStateClosed},
		flow.mapToCloseConversationInput(DuplicateConversation{ConversationID: "30", EarlierConversationID: "10"}))
}

func newUnitTestConnection(t *testing.T, connectionName string) intercom.Connection {
	t.Helper()
	reference := sdkgo.ConnectionRef{Provider: "intercom", Name: connectionName}
	client, err := intercom.New(intercom.Config{}, sdkgo.StaticCredentialProvider[intercom.Credentials]{
		reference: {AccessToken: sdkgo.NewSecretString("intercomUnitTestToken0123456789")},
	})
	require.NoError(t, err)
	connection, err := intercom.NewConnection(client, reference)
	require.NoError(t, err)
	return connection
}
