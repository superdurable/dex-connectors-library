// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package conversationtriage triages one Front conversation: it reads the conversation, finds the
// requester's contact and other open conversations, adds one internal triage comment, and assigns and
// tags the conversation with the teammate and tag chosen in Dex Web. It uses every Front operation.
package conversationtriage

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity shown in Dex Web.
	FlowType = "FrontConversationTriage"
	// ConnectionName is the static Dex Web connection that calls Front.
	ConnectionName = "front-support"
	// TriageCommentPrefix starts every triage comment, so a second Flow for the conversation finds it and stops.
	TriageCommentPrefix = "Dex triage:"
	// NewestMessageLimit is how many of the conversation's newest messages the Flow reads. It reads the
	// front.MaxCommentLimit newest comments, so the skip check sees a triage comment unless that many came after it.
	NewestMessageLimit = 5

	readConversationStepType        = "ReadConversation"
	findRequesterStepType           = "FindRequester"
	searchOpenConversationsStepType = "SearchOpenConversations"
	addTriageCommentStepType        = "AddTriageComment"
	assignAndTagStepType            = "AssignAndTagConversation"
	maximumListedOtherConversations = 10
)

var (
	triageRequestAttribute = dex.DefineAttribute[TriageRequest]("front-triage-request")
	triageAttribute        = dex.DefineAttribute[Triage]("front-triage")
)

// TriageRequest is the Flow's start input.
type TriageRequest struct {
	// ConversationID is the Front conversation to triage, such as cnv_55c8c149.
	ConversationID string `json:"conversationId"`
}

// SearchConfiguration is the SearchOpenConversations Step's inboxPicker value, saved in Dex Web.
type SearchConfiguration struct {
	// InboxID limits the search for the requester's other open conversations to one inbox; empty searches all.
	InboxID string `json:"inboxId,omitempty"`
}

// RoutingConfiguration is the AssignAndTagConversation Step's teammatePicker and tagPicker values.
type RoutingConfiguration struct {
	// AssigneeID is the teammate who receives triaged conversations; empty keeps the current assignee.
	AssigneeID string `json:"assigneeId,omitempty"`
	// TagID is the tag every triaged conversation receives; it is required.
	TagID string `json:"tagId,omitempty"`
}

// SearchConfigurationRef identifies the SearchOpenConversations Step's saved configuration.
func SearchConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: front.ConnectorID, ConnectionName: ConnectionName,
		OperationID: front.SearchConversationsDefinition.Operation.OperationID, FlowType: FlowType, StepType: searchOpenConversationsStepType,
	}
}

// RoutingConfigurationRef identifies the AssignAndTagConversation Step's saved configuration.
func RoutingConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: front.ConnectorID, ConnectionName: ConnectionName,
		OperationID: front.UpdateConversationDefinition.Operation.OperationID, FlowType: FlowType, StepType: assignAndTagStepType,
	}
}

// TriageContext carries the facts the requester lookup and the search need.
type TriageContext struct {
	// ConversationID is the conversation being triaged.
	ConversationID string `json:"conversationId"`
	// RequesterEmail is the email handle of the conversation's requester.
	RequesterEmail string `json:"requesterEmail"`
}

// TriageComment is the internal comment the Flow adds.
type TriageComment struct {
	// ConversationID is the conversation the comment is added to.
	ConversationID string `json:"conversationId"`
	// Text is the comment text.
	Text string `json:"text"`
}

// Triage is the Flow's durable record, updated by every Step and returned when the Flow completes.
type Triage struct {
	// Stage is read, requester, search, comment, route, completed, skipped, needsReview, or failed.
	Stage string `json:"stage"`
	// Subject is the conversation subject.
	Subject string `json:"subject,omitempty"`
	// Status is the conversation's Front status when the Flow read it.
	Status front.ConversationStatus `json:"status,omitempty"`
	// RequesterEmail is the email handle of the newest inbound message's sender.
	RequesterEmail string `json:"requesterEmail,omitempty"`
	// ContactID is the requester's Front contact, or empty when Front has none.
	ContactID string `json:"contactId,omitempty"`
	// OtherOpenConversationIDs lists the requester's other open conversations on Front's first search page.
	OtherOpenConversationIDs []string `json:"otherOpenConversationIds,omitempty"`
	// HasMoreOpenConversations reports that Front has another search page, so OtherOpenConversationIDs may be
	// incomplete and the comment gives its count as a minimum.
	HasMoreOpenConversations bool `json:"hasMoreOpenConversations,omitempty"`
	// CommentID is the triage comment's ID.
	CommentID string `json:"commentId,omitempty"`
	// AssigneeID is the conversation's assignee after routing, or empty.
	AssigneeID string `json:"assigneeId,omitempty"`
	// TagIDs are the conversation's tag IDs after routing.
	TagIDs []string `json:"tagIds,omitempty"`
	// FailedStep names the Step that stopped triage, by failing the Flow or by completing it as needsReview.
	FailedStep string `json:"failedStep,omitempty"`
	// FailedBranch is the Front branch that stopped triage, such as uncertain for needsReview; empty when
	// the Flow's own check stopped it.
	FailedBranch sdkgo.BranchID `json:"failedBranch,omitempty"`
	// FailureKind is the safe failure category of FailedBranch.
	FailureKind sdkgo.FailureKind `json:"failureKind,omitempty"`
	// FailureMessage is the connector's safe message, which never holds tokens or Front's text.
	FailureMessage string `json:"failureMessage,omitempty"`
}

// Flow triages one Front conversation.
type Flow struct {
	dex.FlowDefaults
	connection front.Connection
	search     sdkgo.ConnectorLoadedConfiguration[SearchConfiguration]
	routing    sdkgo.ConnectorLoadedConfiguration[RoutingConfiguration]
}

// NewFlow binds the Front Connection and the Step configurations loaded once at startup.
func NewFlow(
	connection front.Connection, search sdkgo.ConnectorLoadedConfiguration[SearchConfiguration],
	routing sdkgo.ConnectorLoadedConfiguration[RoutingConfiguration],
) *Flow {
	return &Flow{connection: connection, search: search, routing: routing}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Front connector Steps. Every unwired optional branch fails the Flow.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordTriageRequest{tagID: flow.routing.Value.TagID}),
		dex.DefineStep(front.NewGetConversationStep(front.GetConversationStepConfig[TriageRequest]{
			StepType: readConversationStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "front-read", GroupLabel: "Front reads",
				Explanation: "Read the conversation with its newest messages and internal comments.",
			},
			MapToOperationInput: MapToGetConversationInput,
			Found:               sdkgo.GoTo(inspectConversation{}),
		})),
		dex.DefineStep(inspectConversation{}),
		dex.DefineStep(front.NewFindContactByEmailStep(front.FindContactByEmailStepConfig[TriageContext]{
			StepType: findRequesterStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "front-read", GroupLabel: "Front reads",
				Explanation: "Find the requester's Front contact by the email handle of the newest inbound message.",
			},
			MapToOperationInput: MapToFindContactByEmailInput,
			Found:               sdkgo.GoTo(recordRequester{}),
			NotFound:            sdkgo.GoTo(recordRequester{}),
		})),
		dex.DefineStep(recordRequester{}),
		dex.DefineStep(front.NewSearchConversationsStep(front.SearchConversationsStepConfig[TriageContext]{
			StepType: searchOpenConversationsStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "front-read", GroupLabel: "Front reads",
				Explanation: "List the requester's open conversations, in the chosen inbox when one is configured.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{{
				ID: "searchInbox", UnitID: front.UIUnitInboxPicker, Label: "Inbox to search",
				Description: "Select the Front inbox in which the requester's other open conversations are counted; the picker lists the company's inboxes and stores the inbox ID, such as inb_41w25. Leave it empty to search every inbox the API token can see.",
				Bindings:    []sdkgo.ConnectorUIBinding{{Port: front.UIInboxPickerPortInboxID, JSONPointer: "/inboxId"}},
			}}},
			MapToOperationInput: flow.mapToSearchConversationsInput,
			Searched:            sdkgo.GoTo(recordOpenConversations{}),
		})),
		dex.DefineStep(recordOpenConversations{}),
		dex.DefineStep(front.NewReplyToConversationStep(front.ReplyToConversationStepConfig[TriageComment]{
			StepType: addTriageCommentStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "front-write", GroupLabel: "Front writes",
				Explanation: "Add one internal triage comment that only teammates see; an unconfirmed comment goes to review instead of being sent again.",
			},
			MapToOperationInput: MapToReplyToConversationInput,
			Replied:             sdkgo.GoTo(recordTriageComment{}),
			Uncertain:           sdkgo.GoTo(recordCommentNeedsReview{}),
		})),
		dex.DefineStep(recordTriageComment{}),
		dex.DefineStep(recordCommentNeedsReview{}),
		dex.DefineStep(front.NewUpdateConversationStep(front.UpdateConversationStepConfig[TriageRequest]{
			StepType: assignAndTagStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "front-write", GroupLabel: "Front writes",
				Explanation: "Tag the conversation and assign it to the chosen teammate; a repeated attempt finds both applied and writes nothing.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "triageTag", UnitID: front.UIUnitTagPicker, Label: "Triage tag", Required: true,
					Description: "Select the Front tag every triaged conversation receives; the picker lists the tags the API token can see and stores the tag ID, such as tag_13o8r1. It is required: while it is unset, every Flow fails before calling Front.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: front.UITagPickerPortTagID, JSONPointer: "/tagId"}},
				},
				{
					ID: "triageAssignee", UnitID: front.UIUnitTeammatePicker, Label: "Assignee",
					Description: "Select the Front teammate who receives triaged conversations; the picker lists the company's teammates and stores the teammate ID, such as tea_2thf. Leave it empty to keep each conversation's current assignee.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: front.UITeammatePickerPortTeammateID, JSONPointer: "/assigneeId"}},
				},
			}},
			MapToOperationInput: flow.mapToUpdateConversationInput,
			Updated:             sdkgo.GoTo(completeTriage{}),
		})),
		dex.DefineStep(completeTriage{}),
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

// GetDexSummary returns the request and triage record for the Dex Web run list.
//
// dex:field attribute-key:front-triage-request value-type:json editable:false description:"Conversation"
// dex:field attribute-key:front-triage value-type:json editable:false description:"Triage progress and outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, triage, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-triage-request": request, "front-triage": triage}}, nil
}

// GetDexDisplay returns the request and triage record for the Dex Web run detail.
//
// dex:field attribute-key:front-triage-request value-type:json editable:false description:"Conversation ID"
// dex:field attribute-key:front-triage value-type:json editable:false description:"Requester, other open conversations, comment, assignee, and tags"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, triage, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"front-triage-request": request, "front-triage": triage}}, nil
}

// MapToGetConversationInput reads the conversation with its newest messages and as many comments as the
// connector returns, so the skip check sees an earlier triage comment.
func MapToGetConversationInput(request TriageRequest) front.GetConversationInput {
	return front.GetConversationInput{ConversationID: request.ConversationID, MessageLimit: NewestMessageLimit, CommentLimit: front.MaxCommentLimit}
}

// MapToFindContactByEmailInput looks up the requester's contact.
func MapToFindContactByEmailInput(triageContext TriageContext) front.FindContactByEmailInput {
	return front.FindContactByEmailInput{Email: triageContext.RequesterEmail}
}

// mapToSearchConversationsInput lists the requester's open conversations in the configured inbox.
func (flow *Flow) mapToSearchConversationsInput(triageContext TriageContext) front.SearchConversationsInput {
	return front.SearchConversationsInput{
		InboxID: flow.search.Value.InboxID, Statuses: []front.SearchStatusFilter{front.SearchStatusOpen},
		RecipientEmail: triageContext.RequesterEmail, PageSize: maximumListedOtherConversations + 1,
	}
}

// MapToReplyToConversationInput adds the triage comment as an internal comment, never a customer reply.
func MapToReplyToConversationInput(comment TriageComment) front.ReplyToConversationInput {
	return front.ReplyToConversationInput{ConversationID: comment.ConversationID, Text: comment.Text, IsInternalNote: true}
}

// mapToUpdateConversationInput adds the configured tag and, when one is configured, assigns the teammate.
func (flow *Flow) mapToUpdateConversationInput(request TriageRequest) front.UpdateConversationInput {
	return front.UpdateConversationInput{
		ConversationID: request.ConversationID, AssigneeID: flow.routing.Value.AssigneeID, AddTagIDs: []string{flow.routing.Value.TagID},
	}
}

// ChooseRequesterEmail returns the email handle of the newest inbound message's sender, falling back to
// the conversation's main recipient, or empty when neither is an email address.
func ChooseRequesterEmail(details front.ConversationDetails) string {
	for _, message := range details.Messages {
		if !message.IsInbound {
			continue
		}
		for _, recipient := range message.Recipients {
			if recipient.Role == "from" && strings.Contains(recipient.Handle, "@") {
				return recipient.Handle
			}
		}
	}
	if recipient := details.Conversation.Recipient; recipient != nil && strings.Contains(recipient.Handle, "@") {
		return recipient.Handle
	}
	return ""
}

// HasTriageComment reports a comment an earlier triage Flow added, so the conversation is not triaged twice.
func HasTriageComment(details front.ConversationDetails) bool {
	return slices.ContainsFunc(details.Comments, func(comment front.Comment) bool { return strings.HasPrefix(comment.Body, TriageCommentPrefix) })
}

// ListOtherOpenConversations returns the search page's conversations other than the triaged one, and
// whether Front has another page, in which case the list may be incomplete.
func ListOtherOpenConversations(page front.SearchConversationsOutput, triagedConversationID string) ([]string, bool) {
	otherConversationIDs := []string{}
	for _, conversation := range page.Conversations {
		if conversation.ID != triagedConversationID && !slices.Contains(otherConversationIDs, conversation.ID) {
			otherConversationIDs = append(otherConversationIDs, conversation.ID)
		}
	}
	return otherConversationIDs, page.NextPageToken != ""
}

// BuildTriageComment writes the internal comment from the requester's contact and other open conversations.
// It lists at most 10 of them; hasMoreOpenConversations turns the count into a minimum.
func BuildTriageComment(email string, contactID string, otherConversationIDs []string, hasMoreOpenConversations bool) string {
	requester := fmt.Sprintf("the requester %s has no Front contact", email)
	if contactID != "" {
		requester = fmt.Sprintf("the requester %s is Front contact %s", email, contactID)
	}
	if len(otherConversationIDs) == 0 {
		if hasMoreOpenConversations {
			return TriageCommentPrefix + " " + requester + "; Front's first search page listed no other open conversation, and more results exist."
		}
		return TriageCommentPrefix + " " + requester + " and no other open conversation."
	}
	listed := otherConversationIDs[:min(len(otherConversationIDs), maximumListedOtherConversations)]
	others := fmt.Sprintf("%d other open conversations", len(otherConversationIDs))
	if len(otherConversationIDs) == 1 {
		others = "1 other open conversation"
	}
	if hasMoreOpenConversations {
		others = "at least " + others
	}
	if hasMoreOpenConversations || len(listed) < len(otherConversationIDs) {
		others += ", including"
	}
	return fmt.Sprintf("%s %s and has %s: %s.", TriageCommentPrefix, requester, others, strings.Join(listed, ", "))
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

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Check the configured triage tag and persist the conversation before reading it from Front."
type recordTriageRequest struct {
	dex.StepDefaultsNoWaitFor[TriageRequest]
	tagID string
}

func (recordTriageRequest) GetStepType() string { return "RecordTriageRequest" }

func (step recordTriageRequest) Execute(ctx dex.Context, request TriageRequest) (*dex.StepDecision, error) {
	if !strings.HasPrefix(request.ConversationID, "cnv_") {
		return dex.ForceFail("a triage request requires a Front conversation ID such as cnv_55c8c149"), nil
	}
	if step.tagID == "" {
		return dex.ForceFail("the triage tag is not configured; choose it with the tagPicker of the " + assignAndTagStepType + " Step in Dex Web and restart"), nil
	}
	if err := errors.Join(triageRequestAttribute.Set(ctx, request), triageAttribute.Set(ctx, Triage{Stage: "read"})); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageRequest](readConversationStepType), request), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the conversation, skip one an earlier Flow triaged, and continue with the requester's email."
type inspectConversation struct {
	dex.StepDefaultsNoWaitFor[front.GetConversationResult]
}

func (inspectConversation) GetStepType() string { return "InspectConversation" }

func (inspectConversation) Execute(ctx dex.Context, result front.GetConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	conversation := result.Value.Conversation
	triage.Subject, triage.Status = conversation.Subject, conversation.Status
	if HasTriageComment(result.Value) {
		triage.Stage = "skipped"
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(triage), nil
	}
	triage.Stage, triage.RequesterEmail = "requester", ChooseRequesterEmail(result.Value)
	if triage.RequesterEmail == "" {
		triage.Stage, triage.FailedStep, triage.FailureMessage = "failed", readConversationStepType, "the conversation has no inbound email sender to triage"
		if err := triageAttribute.Set(ctx, triage); err != nil {
			return nil, err
		}
		return dex.ForceFail(triage.FailureMessage), nil
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageContext](findRequesterStepType), TriageContext{
		ConversationID: conversation.ID, RequesterEmail: triage.RequesterEmail,
	}), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the requester's contact ID, or none for notFound, and search the requester's open conversations."
type recordRequester struct {
	dex.StepDefaultsNoWaitFor[front.FindContactByEmailResult]
}

func (recordRequester) GetStepType() string { return "RecordRequester" }

func (recordRequester) Execute(ctx dex.Context, result front.FindContactByEmailResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage = "search"
	if len(result.Value.Contacts) > 0 {
		triage.ContactID = result.Value.Contacts[0].ID
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageContext](searchOpenConversationsStepType), TriageContext{
		ConversationID: request.ConversationID, RequesterEmail: triage.RequesterEmail,
	}), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the requester's other open conversations and write the internal triage comment."
type recordOpenConversations struct {
	dex.StepDefaultsNoWaitFor[front.SearchConversationsResult]
}

func (recordOpenConversations) GetStepType() string { return "RecordOpenConversations" }

func (recordOpenConversations) Execute(ctx dex.Context, result front.SearchConversationsResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage = "comment"
	triage.OtherOpenConversationIDs, triage.HasMoreOpenConversations = ListOtherOpenConversations(result.Value, request.ConversationID)
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageComment](addTriageCommentStepType), TriageComment{
		ConversationID: request.ConversationID,
		Text:           BuildTriageComment(triage.RequesterEmail, triage.ContactID, triage.OtherOpenConversationIDs, triage.HasMoreOpenConversations),
	}), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the triage comment's ID and assign and tag the conversation."
type recordTriageComment struct {
	dex.StepDefaultsNoWaitFor[front.ReplyToConversationResult]
}

func (recordTriageComment) GetStepType() string { return "RecordTriageComment" }

func (recordTriageComment) Execute(ctx dex.Context, result front.ReplyToConversationResult) (*dex.StepDecision, error) {
	request, err := triageRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.CommentID = "route", result.Value.CommentID
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageRequest](assignAndTagStepType), request), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Complete with needsReview when the comment's outcome is unknown, without sending it again or routing the conversation."
type recordCommentNeedsReview struct {
	dex.StepDefaultsNoWaitFor[front.ReplyToConversationResult]
}

func (recordCommentNeedsReview) GetStepType() string { return "RecordCommentNeedsReview" }

func (recordCommentNeedsReview) Execute(ctx dex.Context, result front.ReplyToConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	triage.Stage, triage.FailedStep, triage.FailedBranch = "needsReview", addTriageCommentStepType, result.Branch
	if result.Failure != nil {
		triage.FailureKind, triage.FailureMessage = result.Failure.Kind, result.Failure.Message
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

// dex:group group-id:triage group-label:"Triage"
// dex:explanation text:"Record the conversation's assignee and tags after routing and complete with the triage record."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[front.UpdateConversationResult]
}

func (completeTriage) GetStepType() string { return "CompleteTriage" }

func (completeTriage) Execute(ctx dex.Context, result front.UpdateConversationResult) (*dex.StepDecision, error) {
	triage, err := triageAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	conversation := result.Value.Conversation
	triage.Stage, triage.Status, triage.AssigneeID, triage.TagIDs = "completed", conversation.Status, "", []string{}
	if conversation.Assignee != nil {
		triage.AssigneeID = conversation.Assignee.ID
	}
	for _, tag := range conversation.Tags {
		triage.TagIDs = append(triage.TagIDs, tag.ID)
	}
	if err := triageAttribute.Set(ctx, triage); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(triage), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
