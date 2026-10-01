// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package conversationtriage starts one Flow per new Help Scout conversation from the conversationEvent
// Trigger, reads the conversation, looks up the customer's profiles and other active conversations, adds
// one internal triage note, and tags the conversation. It uses every Help Scout operation.
package conversationtriage

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/connectors/helpscout"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "HelpScoutConversationTriage"
	// ConnectionName is the static Dex Web connection that verifies webhooks and calls Help Scout.
	ConnectionName = "helpscout-support"
	// NewConversationTriggerBinding is the conversationEvent binding whose convo.created events start this Flow.
	NewConversationTriggerBinding = "new-conversations"
	// FlowIDPrefix precedes the Trigger event ID in every Flow ID, so a redelivered webhook maps to one Flow.
	FlowIDPrefix = "helpscout-"
	// TriagedTag marks a conversation this Flow triaged.
	TriagedTag = "dex-triaged"
	// RepeatContactTag marks a conversation whose customer has another active conversation in the inbox.
	RepeatContactTag = "dex-repeat-contact"
	// NewestThreadLimit is how many of the conversation's newest threads the Flow reads.
	NewestThreadLimit = 5

	readConversationStepType        = "ReadConversation"
	findCustomerProfilesStepType    = "FindCustomerProfiles"
	searchActiveConversationsStep   = "SearchActiveConversations"
	addTriageNoteStepType           = "AddTriageNote"
	tagTriagedConversationStepType  = "TagTriagedConversation"
	maximumListedOtherConversations = 10
)

var (
	triageRequestAttribute = dex.DefineAttribute[TriageRequest]("helpscout-triage-request")
	triageAttribute        = dex.DefineAttribute[Triage]("helpscout-triage")
)

// TriageRequest is the Flow's start input: one verified convo.created webhook.
type TriageRequest struct {
	// EventID is the Trigger event ID, such as convo.created:501:<body digest>.
	EventID string `json:"eventId"`
	// ConversationID is the new conversation's ID.
	ConversationID int64 `json:"conversationId"`
	// MailboxID is the inbox that received the conversation.
	MailboxID int64 `json:"mailboxId"`
	// Subject is the conversation subject the webhook carried.
	Subject string `json:"subject,omitempty"`
	// ReceivedAt is when the webhook endpoint received the event.
	ReceivedAt time.Time `json:"receivedAt"`
}

// TriageContext carries the facts a later Help Scout call needs: the conversation, its inbox, and the
// customer email read from the conversation.
type TriageContext struct {
	// ConversationID is the conversation being triaged.
	ConversationID int64 `json:"conversationId"`
	// MailboxID is the conversation's inbox.
	MailboxID int64 `json:"mailboxId"`
	// CustomerEmail is the primary customer's email address.
	CustomerEmail string `json:"customerEmail"`
	// IsRepeatContact reports that the customer has another active conversation in the inbox.
	IsRepeatContact bool `json:"isRepeatContact,omitempty"`
}

// TriageNote is the internal note the Flow adds.
type TriageNote struct {
	// ConversationID is the conversation the note is added to.
	ConversationID int64 `json:"conversationId"`
	// Text is the note text.
	Text string `json:"text"`
}

// Triage is the Flow's durable record, updated by every Step and returned when the Flow completes.
type Triage struct {
	// Stage is read, customer, search, note, tag, completed, or failed.
	Stage string `json:"stage"`
	// Subject is the conversation subject.
	Subject string `json:"subject,omitempty"`
	// Status is the conversation's Help Scout status when the Flow read it.
	Status helpscout.ConversationStatus `json:"status,omitempty"`
	// NewestThreadTypes lists the types of the newest threads, newest first, such as customer.
	NewestThreadTypes []string `json:"newestThreadTypes,omitempty"`
	// CustomerEmail is the primary customer's email address.
	CustomerEmail string `json:"customerEmail,omitempty"`
	// CustomerProfileIDs lists every Help Scout customer profile with that email.
	CustomerProfileIDs []int64 `json:"customerProfileIds,omitempty"`
	// OtherActiveConversationIDs lists the customer's other active conversations in the inbox.
	OtherActiveConversationIDs []int64 `json:"otherActiveConversationIds,omitempty"`
	// NoteThreadID is the internal note's thread ID.
	NoteThreadID int64 `json:"noteThreadId,omitempty"`
	// Tags is the conversation's tags after tagging.
	Tags []string `json:"tags,omitempty"`
	// FailedStep names the Step whose branch failed the Flow.
	FailedStep string `json:"failedStep,omitempty"`
	// FailedBranch is the Help Scout branch that failed the Flow.
	FailedBranch sdkgo.BranchID `json:"failedBranch,omitempty"`
	// FailureKind is the safe failure category of FailedBranch.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the connector's safe message, which never holds tokens or Help Scout text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// Flow triages one new Help Scout conversation.
type Flow struct {
	dex.FlowDefaults
	connection helpscout.Connection
}

// NewFlow binds the Help Scout Connection that every Step uses.
func NewFlow(connection helpscout.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the record, read, lookup, search, note, tag, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTriageRequest{}),
		dex.DefineStep(helpscout.NewGetConversationStep(helpscout.GetConversationStepConfig[TriageRequest]{
			StepType: readConversationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "helpscout-read", GroupLabel: "Help Scout reads",
				Explanation: "Read the new conversation and its newest threads from Help Scout.",
			},
			MapToOperationInput: MapToGetConversationInput,
			Found:               sdkgo.GoTo(inspectConversation{}),
			Merged:              sdkgo.GoTo(recordConversationReadFailure{}),
			NotFound:            sdkgo.GoTo(recordConversationReadFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordConversationReadFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordConversationReadFailure{}),
			Defect:              sdkgo.GoTo(recordConversationReadFailure{}),
		})),
		dex.DefineStep(inspectConversation{}),
		dex.DefineStep(recordConversationReadFailure{}),
		dex.DefineStep(helpscout.NewFindCustomerByEmailStep(helpscout.FindCustomerByEmailStepConfig[TriageContext]{
			StepType: findCustomerProfilesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "helpscout-read", GroupLabel: "Help Scout reads",
				Explanation: "Find every Help Scout customer profile with the conversation's customer email.",
			},
			MapToOperationInput: MapToFindCustomerByEmailInput,
			Found:               sdkgo.GoTo(recordCustomerProfiles{}),
			NotFound:            sdkgo.GoTo(recordCustomerProfiles{}),
			ProviderRejected:    sdkgo.GoTo(recordCustomerLookupFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordCustomerLookupFailure{}),
			Defect:              sdkgo.GoTo(recordCustomerLookupFailure{}),
		})),
		dex.DefineStep(recordCustomerProfiles{}),
		dex.DefineStep(recordCustomerLookupFailure{}),
		dex.DefineStep(helpscout.NewSearchConversationsStep(helpscout.SearchConversationsStepConfig[TriageContext]{
			StepType: searchActiveConversationsStep, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "helpscout-read", GroupLabel: "Help Scout reads",
				Explanation: "List the customer's active conversations in the same inbox.",
			},
			MapToOperationInput: MapToSearchConversationsInput,
			Searched:            sdkgo.GoTo(recordActiveConversations{}),
			ProviderRejected:    sdkgo.GoTo(recordConversationSearchFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordConversationSearchFailure{}),
			Defect:              sdkgo.GoTo(recordConversationSearchFailure{}),
		})),
		dex.DefineStep(recordActiveConversations{}),
		dex.DefineStep(recordConversationSearchFailure{}),
		dex.DefineStep(helpscout.NewReplyToConversationStep(helpscout.ReplyToConversationStepConfig[TriageNote]{
			StepType: addTriageNoteStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "helpscout-write", GroupLabel: "Help Scout writes",
				Explanation: "Add one internal triage note that only the team sees, sent at most once.",
			},
			MapToOperationInput: MapToReplyToConversationInput,
			Replied:             sdkgo.GoTo(recordTriageNote{}),
			NotFound:            sdkgo.GoTo(recordTriageNoteFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordTriageNoteFailure{}),
			Uncertain:           sdkgo.GoTo(recordTriageNoteFailure{}),
			Defect:              sdkgo.GoTo(recordTriageNoteFailure{}),
		})),
		dex.DefineStep(recordTriageNote{}),
		dex.DefineStep(recordTriageNoteFailure{}),
		dex.DefineStep(helpscout.NewUpdateConversationStep(helpscout.UpdateConversationStepConfig[TriageContext]{
			StepType: tagTriagedConversationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "helpscout-write", GroupLabel: "Help Scout writes",
				Explanation: "Tag the conversation as triaged, and as a repeat contact when the customer has another active conversation.",
			},
			MapToOperationInput: MapToUpdateConversationInput,
			Updated:             sdkgo.GoTo(completeTriage{}),
			NotFound:            sdkgo.GoTo(recordTaggingFailure{}),
			ProviderRejected:    sdkgo.GoTo(recordTaggingFailure{}),
			InvalidResponse:     sdkgo.GoTo(recordTaggingFailure{}),
			Defect:              sdkgo.GoTo(recordTaggingFailure{}),
		})),
		dex.DefineStep(completeTriage{}),
		dex.DefineStep(recordTaggingFailure{}),
	}
}

// GetRPCs returns the summary and display RPCs that Dex Web shows.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and triage Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{triageRequestAttribute, triageAttribute}}
}

// GetConnectorTriggerBindings declares the conversationEvent binding that starts this Flow.
func (*Flow) GetConnectorTriggerBindings() []sdkgo.TriggerBindingDefinition {
	return []sdkgo.TriggerBindingDefinition{
		helpscout.DefineConversationEventTriggerBinding(helpscout.ConversationEventTriggerBindingConfig{
			ConnectionName: ConnectionName, BindingName: NewConversationTriggerBinding,
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "inbox", UnitID: helpscout.UIUnitMailboxPicker, Label: "Inbox",
				Description: "Select the Help Scout inbox whose new conversations start this Flow; the picker lists the inboxes the connected user can access and stores the inbox ID. Leave it empty to triage new conversations of every inbox.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: helpscout.UIMailboxPickerPortMailboxID, JSONPointer: "/mailboxId"}},
			}}},
		}),
	}
}

// GetDexSummary returns the request and triage record for the Dex Web run list.
//
// dex:field attribute-key:helpscout-triage-request value-type:json editable:false description:"New conversation"
// dex:field attribute-key:helpscout-triage value-type:json editable:false description:"Triage progress and outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, triage, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"helpscout-triage-request": request, "helpscout-triage": triage}}, nil
}

// GetDexDisplay returns the request and triage record for the Dex Web run detail.
//
// dex:field attribute-key:helpscout-triage-request value-type:json editable:false description:"Conversation ID, inbox, and event ID"
// dex:field attribute-key:helpscout-triage value-type:json editable:false description:"Customer profiles, other active conversations, note, and tags"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, triage, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"helpscout-triage-request": request, "helpscout-triage": triage}}, nil
}

// AcceptNewConversation is the application's admission rule: only a convo.created event for an active
// conversation starts a Flow, so spam and conversations created closed are left alone.
func AcceptNewConversation(event sdkgo.TriggerEvent[helpscout.ConversationEvent]) bool {
	return event.Payload.Event == helpscout.WebhookEventConversationCreated &&
		event.Payload.Conversation.Status == helpscout.ConversationStatusActive
}

// ResolveFlowID derives the Flow ID from the event ID, so every redelivery maps to one Flow.
func ResolveFlowID(event sdkgo.TriggerEvent[helpscout.ConversationEvent]) string {
	return FlowIDPrefix + strings.ReplaceAll(event.ID, ":", "-")
}

// MapToFlowInput copies the verified event into the Flow's start input.
func MapToFlowInput(event sdkgo.TriggerEvent[helpscout.ConversationEvent]) TriageRequest {
	conversation := event.Payload.Conversation
	return TriageRequest{
		EventID: event.ID, ConversationID: conversation.ID, MailboxID: conversation.MailboxID,
		Subject: conversation.Subject, ReceivedAt: event.OccurredAt,
	}
}

// MapToGetConversationInput reads the new conversation with its newest threads.
func MapToGetConversationInput(request TriageRequest) helpscout.GetConversationInput {
	return helpscout.GetConversationInput{ConversationID: request.ConversationID, ThreadLimit: NewestThreadLimit}
}

// MapToFindCustomerByEmailInput looks up the conversation's customer.
func MapToFindCustomerByEmailInput(triageContext TriageContext) helpscout.FindCustomerByEmailInput {
	return helpscout.FindCustomerByEmailInput{Email: triageContext.CustomerEmail}
}

// MapToSearchConversationsInput lists the customer's active conversations in the conversation's inbox.
func MapToSearchConversationsInput(triageContext TriageContext) helpscout.SearchConversationsInput {
	return helpscout.SearchConversationsInput{
		MailboxID: triageContext.MailboxID, Status: helpscout.ConversationStatusActive, CustomerEmail: triageContext.CustomerEmail,
	}
}

// MapToReplyToConversationInput adds the triage note as an internal note, never a customer reply.
func MapToReplyToConversationInput(note TriageNote) helpscout.ReplyToConversationInput {
	return helpscout.ReplyToConversationInput{ConversationID: note.ConversationID, Text: note.Text, IsInternalNote: true}
}

// MapToUpdateConversationInput adds TriagedTag, and RepeatContactTag for a repeat contact, keeping every
// other tag and the conversation's status.
func MapToUpdateConversationInput(triageContext TriageContext) helpscout.UpdateConversationInput {
	return helpscout.UpdateConversationInput{ConversationID: triageContext.ConversationID, AddTags: BuildTriageTags(triageContext)}
}

// BuildTriageTags returns TriagedTag, plus RepeatContactTag when the customer has another active conversation.
func BuildTriageTags(triageContext TriageContext) []string {
	if triageContext.IsRepeatContact {
		return []string{TriagedTag, RepeatContactTag}
	}
	return []string{TriagedTag}
}

// BuildTriageNote writes the internal note from the customer's profiles and other active conversations.
func BuildTriageNote(email string, profileIDs []int64, otherConversationIDs []int64) string {
	profiles := fmt.Sprintf("%d Help Scout customer profiles match %s", len(profileIDs), email)
	if len(profileIDs) == 1 {
		profiles = fmt.Sprintf("1 Help Scout customer profile matches %s", email)
	}
	if len(otherConversationIDs) == 0 {
		return "Dex triage: " + profiles + "; the customer has no other active conversation in this inbox."
	}
	listed := make([]string, 0, min(len(otherConversationIDs), maximumListedOtherConversations))
	for _, conversationID := range otherConversationIDs[:min(len(otherConversationIDs), maximumListedOtherConversations)] {
		listed = append(listed, fmt.Sprintf("%d", conversationID))
	}
	others := fmt.Sprintf("%d other active conversations", len(otherConversationIDs))
	if len(otherConversationIDs) == 1 {
		others = "1 other active conversation"
	}
	return fmt.Sprintf("Dex triage: %s; the customer has %s in this inbox: %s.", profiles, others, strings.Join(listed, ", "))
}

func triageInspection(ctx dex.Context) (TriageRequest, Triage, error) {
	request, err := optionalAttribute(ctx, triageRequestAttribute)
	if err != nil {
		return TriageRequest{}, Triage{}, err
	}
	triage, err := optionalAttribute(ctx, triageAttribute)
	if err != nil {
		return TriageRequest{}, Triage{}, err
	}
	return request, triage, nil
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

// describeTriageFailure returns the triage record with the failing branch and the Flow failure message.
func describeTriageFailure(triage Triage, stepType string, branch sdkgo.BranchID, failure *sdkgo.Failure) (Triage, string) {
	triage.Stage, triage.FailedStep, triage.FailedBranch = "failed", stepType, branch
	message := stepType + " selected " + string(branch)
	if failure != nil {
		triage.FailureKind, triage.FailureMessage = failure.Kind, failure.Message
		message += ": " + failure.Message
	}
	return triage, message
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Persist the verified new conversation before reading it from Help Scout."
type recordTriageRequest struct {
	dex.StepDefaultsNoWaitFor[TriageRequest]
}

func (recordTriageRequest) GetStepType() string { return "RecordTriageRequest" }

func (recordTriageRequest) Execute(ctx dex.Context, request TriageRequest) (*dex.StepDecision, error) {
	if request.EventID == "" || request.ConversationID < 1 || request.MailboxID < 1 {
		return dex.ForceFail("a triage request requires its event ID, conversation ID, and inbox ID"), nil
	}
	if err := errors.Join(triageRequestAttribute.Set(ctx, request), triageAttribute.Set(ctx, Triage{Stage: "read", Subject: request.Subject})); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageRequest](readConversationStepType), request), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the conversation's status and newest threads and continue with its primary customer's email."
type inspectConversation struct {
	dex.StepDefaultsNoWaitFor[helpscout.GetConversationResult]
}

func (inspectConversation) GetStepType() string { return "InspectConversation" }

func (inspectConversation) Execute(ctx dex.Context, result helpscout.GetConversationResult) (*dex.StepDecision, error) {
	conversation := result.Value.Conversation
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.Subject, triage.Status = "customer", conversation.Subject, conversation.Status
	triage.NewestThreadTypes = make([]string, 0, len(result.Value.Threads))
	for _, thread := range result.Value.Threads {
		triage.NewestThreadTypes = append(triage.NewestThreadTypes, thread.Type)
	}
	if conversation.PrimaryCustomer == nil || conversation.PrimaryCustomer.Email == "" {
		failed, message := describeTriageFailure(triage, readConversationStepType, result.Branch,
			&sdkgo.Failure{Kind: sdkgo.FailureValidation, Provider: helpscout.ConnectorID, Message: "the conversation has no primary customer email to triage"})
		if err := triageAttribute.Set(ctx, failed); err != nil {
			return nil, err
		}
		return dex.ForceFail(message), nil
	}
	triage.CustomerEmail = conversation.PrimaryCustomer.Email
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageContext](findCustomerProfilesStepType), TriageContext{
		ConversationID: conversation.ID, MailboxID: conversation.MailboxID, CustomerEmail: triage.CustomerEmail,
	}), nil
}

// dex:group group-id:triage-failures group-label:"Triage failures"
// dex:explanation text:"Persist the merged, notFound, providerRejected, invalidResponse, or defect read outcome and fail the Flow."
type recordConversationReadFailure struct {
	dex.StepDefaultsNoWaitFor[helpscout.GetConversationResult]
}

func (recordConversationReadFailure) GetStepType() string { return "RecordConversationReadFailure" }

func (recordConversationReadFailure) Execute(ctx dex.Context, result helpscout.GetConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	failed, message := describeTriageFailure(triage, readConversationStepType, result.Branch, result.Failure)
	if err := triageAttribute.Set(ctx, failed); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the matching customer profile IDs, none for notFound, and search the customer's active conversations."
type recordCustomerProfiles struct {
	dex.StepDefaultsNoWaitFor[helpscout.FindCustomerByEmailResult]
}

func (recordCustomerProfiles) GetStepType() string { return "RecordCustomerProfiles" }

func (recordCustomerProfiles) Execute(ctx dex.Context, result helpscout.FindCustomerByEmailResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.CustomerProfileIDs = "search", make([]int64, 0, len(result.Value.Customers))
	for _, customer := range result.Value.Customers {
		triage.CustomerProfileIDs = append(triage.CustomerProfileIDs, customer.ID)
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageContext](searchActiveConversationsStep), TriageContext{
		ConversationID: request.ConversationID, MailboxID: request.MailboxID, CustomerEmail: triage.CustomerEmail,
	}), nil
}

// dex:group group-id:triage-failures group-label:"Triage failures"
// dex:explanation text:"Persist the providerRejected, invalidResponse, or defect customer lookup outcome and fail the Flow."
type recordCustomerLookupFailure struct {
	dex.StepDefaultsNoWaitFor[helpscout.FindCustomerByEmailResult]
}

func (recordCustomerLookupFailure) GetStepType() string { return "RecordCustomerLookupFailure" }

func (recordCustomerLookupFailure) Execute(ctx dex.Context, result helpscout.FindCustomerByEmailResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	failed, message := describeTriageFailure(triage, findCustomerProfilesStepType, result.Branch, result.Failure)
	if err := triageAttribute.Set(ctx, failed); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the customer's other active conversations and write the internal triage note."
type recordActiveConversations struct {
	dex.StepDefaultsNoWaitFor[helpscout.SearchConversationsResult]
}

func (recordActiveConversations) GetStepType() string { return "RecordActiveConversations" }

func (recordActiveConversations) Execute(ctx dex.Context, result helpscout.SearchConversationsResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.OtherActiveConversationIDs = "note", []int64{}
	for _, conversation := range result.Value.Conversations {
		if conversation.ID != request.ConversationID && !slices.Contains(triage.OtherActiveConversationIDs, conversation.ID) {
			triage.OtherActiveConversationIDs = append(triage.OtherActiveConversationIDs, conversation.ID)
		}
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageNote](addTriageNoteStepType), TriageNote{
		ConversationID: request.ConversationID,
		Text:           BuildTriageNote(triage.CustomerEmail, triage.CustomerProfileIDs, triage.OtherActiveConversationIDs),
	}), nil
}

// dex:group group-id:triage-failures group-label:"Triage failures"
// dex:explanation text:"Persist the providerRejected, invalidResponse, or defect search outcome and fail the Flow."
type recordConversationSearchFailure struct {
	dex.StepDefaultsNoWaitFor[helpscout.SearchConversationsResult]
}

func (recordConversationSearchFailure) GetStepType() string { return "RecordConversationSearchFailure" }

func (recordConversationSearchFailure) Execute(ctx dex.Context, result helpscout.SearchConversationsResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	failed, message := describeTriageFailure(triage, searchActiveConversationsStep, result.Branch, result.Failure)
	if err := triageAttribute.Set(ctx, failed); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the note's thread ID and tag the conversation."
type recordTriageNote struct {
	dex.StepDefaultsNoWaitFor[helpscout.ReplyToConversationResult]
}

func (recordTriageNote) GetStepType() string { return "RecordTriageNote" }

func (recordTriageNote) Execute(ctx dex.Context, result helpscout.ReplyToConversationResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.NoteThreadID = "tag", result.Value.ThreadID
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageContext](tagTriagedConversationStepType), TriageContext{
		ConversationID: request.ConversationID, MailboxID: request.MailboxID, CustomerEmail: triage.CustomerEmail,
		IsRepeatContact: len(triage.OtherActiveConversationIDs) > 0,
	}), nil
}

// dex:group group-id:triage-failures group-label:"Triage failures"
// dex:explanation text:"Persist the uncertain, notFound, providerRejected, or defect note outcome and fail the Flow without sending the note again."
type recordTriageNoteFailure struct {
	dex.StepDefaultsNoWaitFor[helpscout.ReplyToConversationResult]
}

func (recordTriageNoteFailure) GetStepType() string { return "RecordTriageNoteFailure" }

func (recordTriageNoteFailure) Execute(ctx dex.Context, result helpscout.ReplyToConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	failed, message := describeTriageFailure(triage, addTriageNoteStepType, result.Branch, result.Failure)
	if err := triageAttribute.Set(ctx, failed); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the conversation's tags after tagging and complete the Flow with the triage record."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[helpscout.UpdateConversationResult]
}

func (completeTriage) GetStepType() string { return "CompleteTriage" }

func (completeTriage) Execute(ctx dex.Context, result helpscout.UpdateConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.Tags = "completed", result.Value.Conversation.Tags
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

// dex:group group-id:triage-failures group-label:"Triage failures"
// dex:explanation text:"Persist the notFound, providerRejected, invalidResponse, or defect tagging outcome and fail the Flow."
type recordTaggingFailure struct {
	dex.StepDefaultsNoWaitFor[helpscout.UpdateConversationResult]
}

func (recordTaggingFailure) GetStepType() string { return "RecordTaggingFailure" }

func (recordTaggingFailure) Execute(ctx dex.Context, result helpscout.UpdateConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	failed, message := describeTriageFailure(triage, tagTriagedConversationStepType, result.Branch, result.Failure)
	if err := triageAttribute.Set(ctx, failed); err != nil {
		return nil, err
	}
	return dex.ForceFail(message), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
