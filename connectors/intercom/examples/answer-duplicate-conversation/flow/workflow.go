// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package answerduplicate answers an inbound Intercom conversation that duplicates the customer's
// earlier open conversation, then closes it. The conversationEvent Trigger starts one Flow per new
// conversation; the Flow reads it, finds every contact with the sender's email, searches those
// contacts' open and snoozed conversations, and when an earlier one exists, replies once to the new
// conversation pointing the customer to it and closes the new one. Otherwise it writes nothing.
package answerduplicate

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "IntercomAnswerDuplicateConversation"
	// ConnectionName is the static Dex Web connection for the Intercom workspace.
	ConnectionName = "intercom-support-inbox"
	// InboundTriggerBinding is the conversationEvent binding whose events start this Flow.
	InboundTriggerBinding = "inbound-conversation"
	// FlowIDPrefix precedes the conversation ID in every Flow ID, so one conversation starts one Flow.
	FlowIDPrefix = "intercom-duplicate-conversation-"
	// DuplicateReply is the reply the customer sees on the duplicate conversation before it is closed.
	DuplicateReply = "Thanks for getting in touch. We are already working on your earlier conversation with us, " +
		"so we will continue there and close this one."

	recordInboundConversationStepType  = "RecordInboundConversation"
	readInboundConversationStepType    = "ReadInboundConversation"
	chooseCustomerEmailStepType        = "ChooseCustomerEmail"
	findCustomerContactsStepType       = "FindCustomerContacts"
	completeWithoutContactStepType     = "CompleteWithoutContact"
	searchEarlierConversationsStepType = "SearchEarlierConversations"
	chooseEarlierConversationStepType  = "ChooseEarlierConversation"
	replyToDuplicateStepType           = "ReplyToDuplicate"
	recordDuplicateReplyStepType       = "RecordDuplicateReply"
	recordReplyNeedsReviewStepType     = "RecordReplyNeedsReview"
	closeDuplicateStepType             = "CloseDuplicate"
	completeAnsweredDuplicateStepType  = "CompleteAnsweredDuplicate"

	inboundConversationPartLimit      = 10
	earlierConversationSearchPageSize = 20
	maximumSearchedContacts           = intercom.MaxSearchFilterValues

	inboundConversationAttributeKey     = "intercom-inbound-conversation"
	duplicateConversationOutcomeAttrKey = "intercom-duplicate-conversation-outcome"
)

var (
	inboundConversationAttribute = dex.DefineAttribute[InboundConversationState](inboundConversationAttributeKey)
	outcomeAttribute             = dex.DefineAttribute[DuplicateConversationOutcome](duplicateConversationOutcomeAttrKey)
)

// InboundConversation is the Flow's start input: one new conversation from the conversationEvent
// Trigger, or a conversation ID entered in Dex Web Start Flow.
type InboundConversation struct {
	// ConversationID is the Intercom conversation ID, a string of digits.
	ConversationID string `json:"conversationId"`
	// NotificationID is the webhook notification that started the Flow, or empty for Start Flow.
	NotificationID string `json:"notificationId,omitempty"`
	// Topic is the webhook topic, or empty for Start Flow.
	Topic string `json:"topic,omitempty"`
	// WorkspaceID is the Intercom workspace the notification came from, or empty for Start Flow.
	WorkspaceID string `json:"workspaceId,omitempty"`
	// NotifiedAt is when Intercom created the notification, or nil for Start Flow.
	NotifiedAt *time.Time `json:"notifiedAt,omitempty"`
}

// InboundConversationState is the inbound conversation as the Flow learns about it.
type InboundConversationState struct {
	// Input is the validated start input.
	Input InboundConversation `json:"input"`
	// CustomerEmail is the email of the contact who started the conversation, once read.
	CustomerEmail string `json:"customerEmail,omitempty"`
	// CreatedAt is when the inbound conversation was created, once read.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// EarlierConversationID is the customer's earlier open conversation, once found.
	EarlierConversationID string `json:"earlierConversationId,omitempty"`
	// ReplyPartID is the duplicate reply Intercom added, once replied.
	ReplyPartID string `json:"replyPartId,omitempty"`
	// WasReplyAlreadyApplied reports that a retried reply Step found its earlier attempt's reply.
	WasReplyAlreadyApplied bool `json:"wasReplyAlreadyApplied,omitempty"`
}

// ConversationReference names one conversation to read.
type ConversationReference struct {
	// ConversationID is the Intercom conversation ID.
	ConversationID string `json:"conversationId"`
}

// CustomerEmail is the email address whose contacts the Flow looks up.
type CustomerEmail struct {
	// Email is one bare email address.
	Email string `json:"email"`
}

// DuplicateConversation is the inbound conversation to answer and the earlier one it duplicates.
type DuplicateConversation struct {
	// ConversationID is the inbound, duplicate conversation.
	ConversationID string `json:"conversationId"`
	// EarlierConversationID is the customer's earlier open conversation.
	EarlierConversationID string `json:"earlierConversationId"`
}

// ReplyConfiguration is the ReplyToDuplicate Step's use configuration, saved by the adminPicker unit.
type ReplyConfiguration struct {
	// AdminID is the Intercom admin that replies to and closes duplicate conversations.
	AdminID string `json:"adminId"`
}

// OutcomeAction is the Flow's terminal business outcome.
type OutcomeAction string

const (
	// OutcomeAnsweredAndClosed means the duplicate was answered once and closed.
	OutcomeAnsweredAndClosed OutcomeAction = "answeredAndClosed"
	// OutcomeLeftOpen means the conversation is not a duplicate, so nothing was written.
	OutcomeLeftOpen OutcomeAction = "leftOpen"
	// OutcomeSkipped means the conversation was no longer waiting for a first answer, so nothing was written.
	OutcomeSkipped OutcomeAction = "skipped"
	// OutcomeNeedsReview means the reply's outcome could not be confirmed; a teammate checks the conversation.
	OutcomeNeedsReview OutcomeAction = "needsReview"
)

// DuplicateConversationOutcome is the Flow result and the value of its outcome Attribute.
type DuplicateConversationOutcome struct {
	// Action is what the Flow did.
	Action OutcomeAction `json:"action"`
	// Reason explains leftOpen, skipped, and needsReview, such as noEarlierConversation or alreadyAnswered.
	Reason string `json:"reason,omitempty"`
	// ConversationID is the inbound conversation.
	ConversationID string `json:"conversationId"`
	// EarlierConversationID is the earlier conversation the inbound one duplicates, when found.
	EarlierConversationID string `json:"earlierConversationId,omitempty"`
	// ReplyPartID is the reply Intercom added, when it reported the part.
	ReplyPartID string `json:"replyPartId,omitempty"`
	// WasReplyAlreadyApplied reports that a retried reply Step found its earlier attempt's reply.
	WasReplyAlreadyApplied bool `json:"wasReplyAlreadyApplied,omitempty"`
	// WasCloseAlreadyApplied reports that the close Step found the conversation already closed.
	WasCloseAlreadyApplied bool `json:"wasCloseAlreadyApplied,omitempty"`
	// ConversationState is the inbound conversation's Intercom state at the end.
	ConversationState intercom.ConversationState `json:"conversationState,omitempty"`
	// ReviewDetail is the connector's safe failure message for needsReview.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow answers and closes one duplicate inbound conversation.
type Flow struct {
	dex.FlowDefaults
	connection         intercom.Connection
	replyConfiguration sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]
}

// NewFlow binds the Intercom Connection and the ReplyToDuplicate Step's loaded configuration, whose
// admin replies to and closes duplicates. An empty admin fails each Flow before any Intercom request.
func NewFlow(connection intercom.Connection, replyConfiguration sdkgo.ConnectorLoadedConfiguration[ReplyConfiguration]) *Flow {
	return &Flow{connection: connection, replyConfiguration: replyConfiguration}
}

// ReplyConfigurationRef identifies the ReplyToDuplicate Step's use configuration in Dex Web.
func ReplyConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: intercom.ConnectorID, ConnectionName: ConnectionName, OperationID: "replyToConversation",
		FlowType: FlowType, StepType: replyToDuplicateStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Intercom connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordInboundConversation{adminID: flow.replyConfiguration.Value.AdminID}),
		dex.DefineStep(intercom.NewGetConversationStep(intercom.GetConversationStepConfig[ConversationReference]{
			StepType: readInboundConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intercom", GroupLabel: "Intercom",
				Explanation: "Read the new conversation, its first message, and its latest parts.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetConversationInput,
			Found: sdkgo.GoTo(chooseCustomerEmail{}),
		})),
		dex.DefineStep(chooseCustomerEmail{}),
		dex.DefineStep(intercom.NewFindContactByEmailStep(intercom.FindContactByEmailStepConfig[CustomerEmail]{
			StepType: findCustomerContactsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intercom", GroupLabel: "Intercom",
				Explanation: "Find every user and lead with the sender's email, such as a lead and a user for one person.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindContactByEmailInput,
			Found:    sdkgo.GoTo(sdkgo.StepRef[intercom.FindContactByEmailResult](searchEarlierConversationsStepType)),
			NotFound: sdkgo.GoTo(completeWithoutContact{}),
		})),
		dex.DefineStep(completeWithoutContact{}),
		dex.DefineStep(intercom.NewSearchConversationsStep(intercom.SearchConversationsStepConfig[intercom.FindContactByEmailResult]{
			StepType: searchEarlierConversationsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intercom", GroupLabel: "Intercom",
				Explanation: "Search those contacts' open and snoozed conversations for an earlier one.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchConversationsInput,
			Searched: sdkgo.GoTo(chooseEarlierConversation{}),
		})),
		dex.DefineStep(chooseEarlierConversation{}),
		dex.DefineStep(intercom.NewReplyToConversationStep(intercom.ReplyToConversationStepConfig[DuplicateConversation]{
			StepType: replyToDuplicateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intercom", GroupLabel: "Intercom",
				Explanation: "Reply once to the duplicate, pointing the customer to the earlier conversation; an unconfirmed reply is never resent.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "replyingAdmin", UnitID: intercom.UIUnitAdminPicker, Label: "Replying admin", Required: true,
				Description: "Select the Intercom teammate whose name the customer sees on the duplicate reply; the same admin closes the duplicate. The picker stores the admin's numeric ID, and leaving it unset fails every Flow before Intercom is called.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: intercom.UIAdminPickerPortAdminID, JSONPointer: "/adminId"}},
			}}},
			Connection:          flow.connection,
			MapToOperationInput: flow.mapToReplyToConversationInput,
			Replied:             sdkgo.GoTo(recordDuplicateReply{}),
			Uncertain:           sdkgo.GoTo(recordReplyNeedsReview{}),
		})),
		dex.DefineStep(recordDuplicateReply{}),
		dex.DefineStep(recordReplyNeedsReview{}),
		dex.DefineStep(intercom.NewUpdateConversationStateStep(intercom.UpdateConversationStateStepConfig[DuplicateConversation]{
			StepType: closeDuplicateStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "intercom", GroupLabel: "Intercom",
				Explanation: "Close the answered duplicate as the replying admin; a repeated close finds it closed and writes nothing.",
			},
			Connection:          flow.connection,
			MapToOperationInput: flow.mapToCloseConversationInput,
			Updated:             sdkgo.GoTo(completeAnsweredDuplicate{}),
		})),
		dex.DefineStep(completeAnsweredDuplicate{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the inbound conversation and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{inboundConversationAttribute, outcomeAttribute}}
}

// GetConnectorTriggerBindings declares the conversationEvent binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		intercom.DefineConversationEventTriggerBinding(intercom.ConversationEventTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: InboundTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "topics", UnitID: intercom.UIUnitConversationTopicPicker, Label: "Topics that start the Flow",
				Description: "Select conversation.user.created, the topic Intercom sends once for each conversation a customer starts; subscribe the same topic on the Intercom app's Configure > Webhooks page. The Flow itself admits only that topic, so selecting none accepts every supported topic and still starts Flows only for new conversations.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: intercom.UIConversationTopicPickerPortTopics, JSONPointer: "/topics"}},
			}}},
		}),
	}
}

// GetDexSummary returns the inbound conversation and the outcome for the Dex Web run list.
//
// dex:field attribute-key:intercom-inbound-conversation value-type:json editable:false description:"Inbound conversation"
// dex:field attribute-key:intercom-duplicate-conversation-outcome value-type:json editable:false description:"Duplicate outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inbound, outcome, err := duplicateConversationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		inboundConversationAttributeKey: inbound, duplicateConversationOutcomeAttrKey: outcome,
	}}, nil
}

// GetDexDisplay returns the inbound conversation and the outcome for the Dex Web run detail.
//
// dex:field attribute-key:intercom-inbound-conversation value-type:json editable:false description:"Conversation, notification, customer email, and earlier conversation"
// dex:field attribute-key:intercom-duplicate-conversation-outcome value-type:json editable:false description:"Action, reason, reply part, and final state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	inbound, outcome, err := duplicateConversationInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		inboundConversationAttributeKey: inbound, duplicateConversationOutcomeAttrKey: outcome,
	}}, nil
}

// AcceptInboundConversation is the application's admission rule: only a new conversation starts a Flow.
func AcceptInboundConversation(event sdkgo.TriggerEvent[intercom.ConversationEvent]) bool {
	return event.Payload.Topic == intercom.TopicConversationUserCreated && event.Payload.Conversation.ID != ""
}

// ResolveFlowID derives the Flow ID from the conversation ID, so a redelivery starts no second Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[intercom.ConversationEvent]) string {
	return FlowIDPrefix + event.Payload.Conversation.ID
}

// MapToFlowInput copies the notification's identity into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[intercom.ConversationEvent]) InboundConversation {
	notifiedAt := event.Payload.NotifiedAt
	return InboundConversation{
		ConversationID: event.Payload.Conversation.ID, NotificationID: event.Payload.NotificationID,
		Topic: event.Payload.Topic, WorkspaceID: event.Payload.WorkspaceID, NotifiedAt: &notifiedAt,
	}
}

// MapToGetConversationInput reads the inbound conversation with its latest parts.
func MapToGetConversationInput(reference ConversationReference) intercom.GetConversationInput {
	return intercom.GetConversationInput{ConversationID: reference.ConversationID, LatestPartLimit: inboundConversationPartLimit}
}

// MapToFindContactByEmailInput looks up every contact with the sender's email.
func MapToFindContactByEmailInput(customer CustomerEmail) intercom.FindContactByEmailInput {
	return intercom.FindContactByEmailInput{Email: customer.Email, ContactLimit: maximumSearchedContacts}
}

// MapToSearchConversationsInput searches the found contacts' open and snoozed conversations.
func MapToSearchConversationsInput(result intercom.FindContactByEmailResult) intercom.SearchConversationsInput {
	contactIDs := make([]string, 0, len(result.Value.Contacts))
	for _, contact := range result.Value.Contacts {
		if !slices.Contains(contactIDs, contact.ID) && len(contactIDs) < maximumSearchedContacts {
			contactIDs = append(contactIDs, contact.ID)
		}
	}
	return intercom.SearchConversationsInput{
		States:     []intercom.ConversationState{intercom.ConversationStateOpen, intercom.ConversationStateSnoozed},
		ContactIDs: contactIDs, PageSize: earlierConversationSearchPageSize,
	}
}

// mapToReplyToConversationInput replies to the duplicate as the configured admin.
func (flow *Flow) mapToReplyToConversationInput(duplicate DuplicateConversation) intercom.ReplyToConversationInput {
	return intercom.ReplyToConversationInput{
		ConversationID: duplicate.ConversationID, AdminID: flow.replyConfiguration.Value.AdminID,
		MessageType: intercom.ReplyMessageTypeComment, Body: DuplicateReply,
	}
}

// mapToCloseConversationInput closes the answered duplicate as the admin that replied.
func (flow *Flow) mapToCloseConversationInput(duplicate DuplicateConversation) intercom.UpdateConversationStateInput {
	return intercom.UpdateConversationStateInput{
		ConversationID: duplicate.ConversationID, AdminID: flow.replyConfiguration.Value.AdminID, State: intercom.ConversationStateClosed,
	}
}

// ChooseCustomer returns the email to look up, or the skip reason when the conversation is no longer
// waiting for a first answer: closed or snoozed, already answered by an admin, or not started by a contact.
func ChooseCustomer(details intercom.ConversationDetails) (string, string) {
	if details.Conversation.State != intercom.ConversationStateOpen {
		return "", "notOpen"
	}
	for _, part := range details.LatestParts {
		if part.PartType == string(intercom.ReplyMessageTypeComment) && part.Author.Type == "admin" {
			return "", "alreadyAnswered"
		}
	}
	author := details.Conversation.Source.Author
	if (author.Type != "user" && author.Type != "lead" && author.Type != "contact") || strings.TrimSpace(author.Email) == "" {
		return "", "noCustomerEmail"
	}
	return strings.TrimSpace(author.Email), ""
}

// ChooseEarlierConversation returns the oldest other open or snoozed conversation created before the
// inbound one, or false when the inbound conversation is not a duplicate.
func ChooseEarlierConversation(conversations []intercom.Conversation, inbound InboundConversationState) (intercom.Conversation, bool) {
	var chosen intercom.Conversation
	isFound := false
	for _, conversation := range conversations {
		if conversation.ID == inbound.Input.ConversationID || !conversation.CreatedAt.Before(inbound.CreatedAt) {
			continue
		}
		if conversation.State != intercom.ConversationStateOpen && conversation.State != intercom.ConversationStateSnoozed {
			continue
		}
		if !isFound || conversation.CreatedAt.Before(chosen.CreatedAt) {
			chosen, isFound = conversation, true
		}
	}
	return chosen, isFound
}

func duplicateConversationInspection(ctx dex.Context) (InboundConversationState, DuplicateConversationOutcome, error) {
	inbound, err := optionalAttribute(ctx, inboundConversationAttribute)
	if err != nil {
		return InboundConversationState{}, DuplicateConversationOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return InboundConversationState{}, DuplicateConversationOutcome{}, err
	}
	return inbound, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:inbound group-label:"Inbound conversation"
// dex:explanation text:"Validate the conversation ID and the configured replying admin, and record the inbound conversation."
type recordInboundConversation struct {
	dex.StepDefaults
	adminID string
}

func (recordInboundConversation) GetStepType() string { return recordInboundConversationStepType }

// WaitFor skips immediately because Dex Web Start Flow invokes the start Step's WaitFor.
func (recordInboundConversation) WaitFor(dex.Context, InboundConversation) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordInboundConversation) Execute(ctx dex.Context, input InboundConversation) (*dex.StepDecision, error) {
	input.ConversationID = strings.TrimSpace(input.ConversationID)
	if input.ConversationID == "" || strings.Trim(input.ConversationID, "0123456789") != "" {
		return dex.ForceFail("conversationId must be an Intercom conversation ID of digits"), nil
	}
	if step.adminID == "" {
		return dex.ForceFail("choose the replying admin in the ReplyToDuplicate Step's adminPicker in Dex Web, then restart the Worker"), nil
	}
	if err := inboundConversationAttribute.Set(ctx, InboundConversationState{Input: input}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ConversationReference](readInboundConversationStepType), ConversationReference{ConversationID: input.ConversationID}), nil
}

// dex:group group-id:inbound group-label:"Inbound conversation"
// dex:explanation text:"Skip a conversation that is closed, snoozed, or already answered; otherwise look up the sender's email."
type chooseCustomerEmail struct {
	dex.StepDefaultsNoWaitFor[intercom.GetConversationResult]
}

func (chooseCustomerEmail) GetStepType() string { return chooseCustomerEmailStepType }

func (chooseCustomerEmail) Execute(ctx dex.Context, result intercom.GetConversationResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	email, skipReason := ChooseCustomer(result.Value)
	if skipReason != "" {
		outcome := DuplicateConversationOutcome{
			Action: OutcomeSkipped, Reason: skipReason, ConversationID: inbound.Input.ConversationID,
			ConversationState: result.Value.Conversation.State,
		}
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	inbound.CustomerEmail, inbound.CreatedAt = email, result.Value.Conversation.CreatedAt
	if err := inboundConversationAttribute.Set(ctx, inbound); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerEmail](findCustomerContactsStepType), CustomerEmail{Email: email}), nil
}

// dex:group group-id:inbound group-label:"Inbound conversation"
// dex:explanation text:"Leave the conversation open when Intercom has no contact with the sender's email yet."
type completeWithoutContact struct {
	dex.StepDefaultsNoWaitFor[intercom.FindContactByEmailResult]
}

func (completeWithoutContact) GetStepType() string { return completeWithoutContactStepType }

func (completeWithoutContact) Execute(ctx dex.Context, _ intercom.FindContactByEmailResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := DuplicateConversationOutcome{
		Action: OutcomeLeftOpen, Reason: "noContact", ConversationID: inbound.Input.ConversationID,
		ConversationState: intercom.ConversationStateOpen,
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:inbound group-label:"Inbound conversation"
// dex:explanation text:"Answer only when the customer has an earlier open or snoozed conversation; otherwise leave this one open."
type chooseEarlierConversation struct {
	dex.StepDefaultsNoWaitFor[intercom.SearchConversationsResult]
}

func (chooseEarlierConversation) GetStepType() string { return chooseEarlierConversationStepType }

func (chooseEarlierConversation) Execute(ctx dex.Context, result intercom.SearchConversationsResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	earlier, isFound := ChooseEarlierConversation(result.Value.Conversations, inbound)
	if !isFound {
		outcome := DuplicateConversationOutcome{
			Action: OutcomeLeftOpen, Reason: "noEarlierConversation", ConversationID: inbound.Input.ConversationID,
			ConversationState: intercom.ConversationStateOpen,
		}
		if err := outcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	inbound.EarlierConversationID = earlier.ID
	if err := inboundConversationAttribute.Set(ctx, inbound); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DuplicateConversation](replyToDuplicateStepType), DuplicateConversation{
		ConversationID: inbound.Input.ConversationID, EarlierConversationID: earlier.ID,
	}), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record the reply Intercom confirmed, or that a retried attempt found, before closing the duplicate."
type recordDuplicateReply struct {
	dex.StepDefaultsNoWaitFor[intercom.ReplyToConversationResult]
}

func (recordDuplicateReply) GetStepType() string { return recordDuplicateReplyStepType }

func (recordDuplicateReply) Execute(ctx dex.Context, result intercom.ReplyToConversationResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	inbound.ReplyPartID, inbound.WasReplyAlreadyApplied = result.Value.Part.ID, result.Value.WasAlreadyApplied
	if err := inboundConversationAttribute.Set(ctx, inbound); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[DuplicateConversation](closeDuplicateStepType), DuplicateConversation{
		ConversationID: inbound.Input.ConversationID, EarlierConversationID: inbound.EarlierConversationID,
	}), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record an unconfirmed reply for a teammate to check and complete without closing the conversation."
type recordReplyNeedsReview struct {
	dex.StepDefaultsNoWaitFor[intercom.ReplyToConversationResult]
}

func (recordReplyNeedsReview) GetStepType() string { return recordReplyNeedsReviewStepType }

func (recordReplyNeedsReview) Execute(ctx dex.Context, result intercom.ReplyToConversationResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := DuplicateConversationOutcome{
		Action: OutcomeNeedsReview, Reason: "replyUncertain", ConversationID: inbound.Input.ConversationID,
		EarlierConversationID: inbound.EarlierConversationID, ConversationState: result.Value.ConversationState,
	}
	if result.Failure != nil {
		outcome.ReviewDetail = result.Failure.Message
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:outcome group-label:"Outcome"
// dex:explanation text:"Record the answered and closed duplicate as the outcome and complete the Flow."
type completeAnsweredDuplicate struct {
	dex.StepDefaultsNoWaitFor[intercom.UpdateConversationStateResult]
}

func (completeAnsweredDuplicate) GetStepType() string { return completeAnsweredDuplicateStepType }

func (completeAnsweredDuplicate) Execute(ctx dex.Context, result intercom.UpdateConversationStateResult) (*dex.StepDecision, error) {
	inbound, err := inboundConversationAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := DuplicateConversationOutcome{
		Action: OutcomeAnsweredAndClosed, ConversationID: inbound.Input.ConversationID,
		EarlierConversationID: inbound.EarlierConversationID, ReplyPartID: inbound.ReplyPartID,
		WasReplyAlreadyApplied: inbound.WasReplyAlreadyApplied, WasCloseAlreadyApplied: result.Value.WasAlreadyApplied,
		ConversationState: result.Value.Conversation.State,
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
