// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package claimconversation demonstrates every Hiver operation in one Flow started from Dex Web
// Start Flow: find a shared inbox by its email address, claim its first open unassigned
// conversation for a Hiver user, tag it, add one internal note, and optionally leave a shared
// reply draft for that user to review and send.
package claimconversation

import (
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/hiver"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "HiverClaimConversation"
	// ConnectionName is the static Dex Web connection for Hiver.
	ConnectionName = "hiver-account"

	recordClaimRequestStepType         = "RecordClaimRequest"
	findSharedInboxStepType            = "FindSharedInbox"
	chooseSharedInboxStepType          = "ChooseSharedInbox"
	findUnassignedConversationStepType = "FindUnassignedConversation"
	chooseConversationStepType         = "ChooseConversation"
	readConversationStepType           = "ReadConversation"
	confirmConversationStepType        = "ConfirmConversation"
	claimConversationStepType          = "ClaimConversation"
	recordClaimedConversationStepType  = "RecordClaimedConversation"
	addClaimNoteStepType               = "AddClaimNote"
	recordClaimNoteStepType            = "RecordClaimNote"
	recordUncertainNoteStepType        = "RecordUncertainNote"
	draftCustomerReplyStepType         = "DraftCustomerReply"
	completeClaimStepType              = "CompleteClaim"
	recordUncertainDraftStepType       = "RecordUncertainDraft"

	// MaximumInboxPages bounds the inbox search to 500 shared inboxes.
	MaximumInboxPages = 5
	// MaximumConversationPages bounds the conversation scan to 250 conversations.
	MaximumConversationPages = 5
	conversationPageSize     = 50

	maximumNoteBytes  = 8192
	maximumDraftBytes = 32768

	addClaimNoteReviewReason       = "addNote"
	draftCustomerReplyReviewReason = "createSharedDraft"
)

var (
	claimRequestAttribute = dex.DefineAttribute[ClaimRequest]("hiver-claim-request")
	claimOutcomeAttribute = dex.DefineAttribute[ClaimOutcome]("hiver-claim-outcome")
)

// Input is the claim entered in Dex Web Start Flow.
type Input struct {
	// InboxEmail is the shared mailbox address of the Hiver inbox, such as support@acme.example.com.
	InboxEmail string `json:"inboxEmail"`
	// AssigneeEmail is the email of the Hiver user who takes the conversation.
	AssigneeEmail string `json:"assigneeEmail"`
	// ClaimTagName names an existing tag of the inbox to apply, such as Claimed; empty applies none.
	ClaimTagName string `json:"claimTagName,omitempty"`
	// Note is the internal note added to the claimed conversation.
	Note string `json:"note"`
	// ReplyDraft is a reply the assignee reviews and sends from Hiver; empty creates no draft.
	ReplyDraft string `json:"replyDraft,omitempty"`
}

// ClaimRequest is the validated claim every later Step reads.
type ClaimRequest struct {
	// InboxEmail is the shared mailbox address, lowercase.
	InboxEmail string `json:"inboxEmail"`
	// AssigneeEmail is the Hiver user who takes the conversation.
	AssigneeEmail string `json:"assigneeEmail"`
	// ClaimTagName is the inbox tag to apply, or empty.
	ClaimTagName string `json:"claimTagName,omitempty"`
	// Note is the internal note.
	Note string `json:"note"`
	// ReplyDraft is the reply draft, or empty.
	ReplyDraft string `json:"replyDraft,omitempty"`
}

// InboxSearch is one page of the search for the shared inbox.
type InboxSearch struct {
	// PageToken is Hiver's next-page token, empty for the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// ConversationSearch is one page of the scan for an open unassigned conversation.
type ConversationSearch struct {
	// InboxID is the Hiver shared inbox ID.
	InboxID string `json:"inboxId"`
	// PageToken is Hiver's next-page token, empty for the first page.
	PageToken string `json:"pageToken,omitempty"`
}

// ConversationReference identifies the candidate conversation.
type ConversationReference struct {
	// InboxID is the Hiver shared inbox ID.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID.
	ConversationID string `json:"conversationId"`
}

// ConversationClaim assigns and tags the confirmed conversation.
type ConversationClaim struct {
	// InboxID is the Hiver shared inbox ID.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID.
	ConversationID string `json:"conversationId"`
	// AssigneeEmail is the Hiver user who takes the conversation.
	AssigneeEmail string `json:"assigneeEmail"`
	// ClaimTagName is the inbox tag to apply, or empty.
	ClaimTagName string `json:"claimTagName,omitempty"`
}

// ClaimNote is the internal note to add.
type ClaimNote struct {
	// InboxID is the Hiver shared inbox ID.
	InboxID string `json:"inboxId"`
	// ConversationID is the Hiver conversation ID.
	ConversationID string `json:"conversationId"`
	// Content is the note text.
	Content string `json:"content"`
}

// ReplyDraft is the shared draft to create.
type ReplyDraft struct {
	// InboxID is the Hiver shared inbox ID.
	InboxID string `json:"inboxId"`
	// HiverMessageID is the Hiver message the draft replies to, or empty when only GmailMessageID is known.
	HiverMessageID string `json:"hiverMessageId,omitempty"`
	// GmailMessageID is the Gmail message the draft replies to when HiverMessageID is empty.
	GmailMessageID string `json:"gmailMessageId,omitempty"`
	// Body is the draft text.
	Body string `json:"body"`
}

// ClaimAction is what the Flow did.
type ClaimAction string

const (
	// ClaimActionClaimed means a conversation was assigned, tagged, and noted.
	ClaimActionClaimed ClaimAction = "claimed"
	// ClaimActionNothingToClaim means the scanned pages held no open unassigned conversation.
	ClaimActionNothingToClaim ClaimAction = "nothingToClaim"
	// ClaimActionClaimedElsewhere means the candidate was assigned or no longer open when read.
	ClaimActionClaimedElsewhere ClaimAction = "claimedElsewhere"
)

// ClaimOutcome is the Flow result and the value of its outcome Attribute.
type ClaimOutcome struct {
	// Action is what the Flow did.
	Action ClaimAction `json:"action,omitempty"`
	// InboxID is the Hiver shared inbox the email address resolved to.
	InboxID string `json:"inboxId,omitempty"`
	// InboxPagesRead counts the inbox pages read, at most MaximumInboxPages.
	InboxPagesRead int `json:"inboxPagesRead,omitempty"`
	// ConversationPagesRead counts the conversation pages read, at most MaximumConversationPages.
	ConversationPagesRead int `json:"conversationPagesRead,omitempty"`
	// Conversation is the claimed conversation as read back after the change, or the candidate
	// as read when it was claimed elsewhere.
	Conversation *hiver.Conversation `json:"conversation,omitempty"`
	// ReplyToMessage is the conversation's last listed message, which a reply draft answers.
	ReplyToMessage *hiver.ConversationMessage `json:"replyToMessage,omitempty"`
	// NoteID is the internal note Hiver added.
	NoteID string `json:"noteId,omitempty"`
	// SharedDraftID is the shared reply draft Hiver created.
	SharedDraftID string `json:"sharedDraftId,omitempty"`
	// NeedsReview reports that a note or draft was sent with an unknown outcome, so a person must
	// check Hiver; the Flow never resends it.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the uncertain write: addNote or createSharedDraft.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow claims one conversation of a Hiver shared inbox.
type Flow struct {
	dex.FlowDefaults
	connection hiver.Connection
}

// NewFlow binds the Hiver Connection at registration time.
func NewFlow(connection hiver.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Hiver connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordClaimRequest{}),
		dex.DefineStep(hiver.NewListInboxesStep(hiver.ListInboxesStepConfig[InboxSearch]{
			StepType: findSharedInboxStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "List one page of the account's shared inboxes."},
			Connection:  flow.connection, MapToOperationInput: MapToListInboxesInput,
			Listed: sdkgo.GoTo(chooseSharedInbox{}),
		})),
		dex.DefineStep(chooseSharedInbox{}),
		dex.DefineStep(hiver.NewListConversationsStep(hiver.ListConversationsStepConfig[ConversationSearch]{
			StepType: findUnassignedConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "List one page of the shared inbox's conversations."},
			Connection:  flow.connection, MapToOperationInput: MapToListConversationsInput,
			Listed: sdkgo.GoTo(chooseConversation{}),
		})),
		dex.DefineStep(chooseConversation{}),
		dex.DefineStep(hiver.NewGetConversationStep(hiver.GetConversationStepConfig[ConversationReference]{
			StepType: readConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "Read the candidate conversation with its message IDs before changing it."},
			Connection:  flow.connection, MapToOperationInput: MapToGetConversationInput,
			Found: sdkgo.GoTo(confirmConversation{}),
		})),
		dex.DefineStep(confirmConversation{}),
		dex.DefineStep(hiver.NewUpdateConversationStep(hiver.UpdateConversationStepConfig[ConversationClaim]{
			StepType: claimConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "hiver", GroupLabel: "Hiver",
				Explanation: "Assign the conversation and apply the claim tag, then read it back; a repeated attempt sends the same change.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateConversationInput,
			Updated: sdkgo.GoTo(recordClaimedConversation{}),
		})),
		dex.DefineStep(recordClaimedConversation{}),
		dex.DefineStep(hiver.NewAddNoteStep(hiver.AddNoteStepConfig[ClaimNote]{
			StepType: addClaimNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "Add one internal note; a retry that finds the dispatch checkpoint selects uncertain instead of resending."},
			Connection:  flow.connection, MapToOperationInput: MapToAddNoteInput,
			Added: sdkgo.GoTo(recordClaimNote{}), Uncertain: sdkgo.GoTo(recordUncertainNote{}),
		})),
		dex.DefineStep(recordClaimNote{}),
		dex.DefineStep(recordUncertainNote{}),
		dex.DefineStep(hiver.NewCreateSharedDraftStep(hiver.CreateSharedDraftStepConfig[ReplyDraft]{
			StepType: draftCustomerReplyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "hiver", GroupLabel: "Hiver", Explanation: "Create one shared reply draft for the assignee; a retry that finds the dispatch checkpoint selects uncertain instead of resending."},
			Connection:  flow.connection, MapToOperationInput: MapToCreateSharedDraftInput,
			Created: sdkgo.GoTo(completeClaim{}), Uncertain: sdkgo.GoTo(recordUncertainDraft{}),
		})),
		dex.DefineStep(completeClaim{}),
		dex.DefineStep(recordUncertainDraft{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{claimRequestAttribute, claimOutcomeAttribute}}
}

// GetDexSummary returns the claim request and its outcome.
//
// dex:field attribute-key:hiver-claim-request value-type:json editable:false description:"Claim request"
// dex:field attribute-key:hiver-claim-outcome value-type:json editable:false description:"Hiver claim outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := claimInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hiver-claim-request": request,
		"hiver-claim-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the claim request and the conversation, note, and draft Hiver returned.
//
// dex:field attribute-key:hiver-claim-request value-type:json editable:false description:"Inbox address, assignee, tag, note, and reply draft"
// dex:field attribute-key:hiver-claim-outcome value-type:json editable:false description:"Action, conversation, note, draft, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := claimInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"hiver-claim-request": request,
		"hiver-claim-outcome": outcome,
	}}, nil
}

// MapToListInboxesInput reads one page of shared inboxes.
func MapToListInboxesInput(search InboxSearch) hiver.ListInboxesInput {
	return hiver.ListInboxesInput{PageToken: search.PageToken, PageSize: hiver.MaximumPageSize}
}

// MapToListConversationsInput reads one page of the inbox's conversations.
func MapToListConversationsInput(search ConversationSearch) hiver.ListConversationsInput {
	return hiver.ListConversationsInput{InboxID: search.InboxID, PageToken: search.PageToken, PageSize: conversationPageSize}
}

// MapToGetConversationInput reads the candidate conversation.
func MapToGetConversationInput(reference ConversationReference) hiver.GetConversationInput {
	return hiver.GetConversationInput{InboxID: reference.InboxID, ConversationID: reference.ConversationID}
}

// MapToUpdateConversationInput assigns the conversation and applies the claim tag when one is named.
func MapToUpdateConversationInput(claim ConversationClaim) hiver.UpdateConversationInput {
	input := hiver.UpdateConversationInput{InboxID: claim.InboxID, ConversationID: claim.ConversationID, AssigneeEmail: claim.AssigneeEmail}
	if claim.ClaimTagName != "" {
		input.ApplyTagNames = []string{claim.ClaimTagName}
	}
	return input
}

// MapToAddNoteInput adds the internal claim note.
func MapToAddNoteInput(note ClaimNote) hiver.AddNoteInput {
	return hiver.AddNoteInput{InboxID: note.InboxID, ConversationID: note.ConversationID, Content: note.Content}
}

// MapToCreateSharedDraftInput drafts the reply to the conversation's last listed message.
func MapToCreateSharedDraftInput(draft ReplyDraft) hiver.CreateSharedDraftInput {
	return hiver.CreateSharedDraftInput{InboxID: draft.InboxID, HiverMessageID: draft.HiverMessageID, GmailMessageID: draft.GmailMessageID, Body: draft.Body}
}

// FindInboxByEmail returns the inbox whose shared mailbox address matches, ignoring letter case.
func FindInboxByEmail(inboxes []hiver.Inbox, email string) (hiver.Inbox, bool) {
	for _, inbox := range inboxes {
		if strings.EqualFold(inbox.Email, email) {
			return inbox, true
		}
	}
	return hiver.Inbox{}, false
}

// FindClaimableConversation returns the first open conversation without an assignee.
func FindClaimableConversation(conversations []hiver.Conversation) (hiver.Conversation, bool) {
	for _, conversation := range conversations {
		if IsClaimable(conversation) {
			return conversation, true
		}
	}
	return hiver.Conversation{}, false
}

// IsClaimable reports whether a conversation is open and unassigned.
func IsClaimable(conversation hiver.Conversation) bool {
	return conversation.Status == hiver.ConversationStatusOpen && conversation.Assignee == nil
}

// BuildClaimNote is the internal note on the claimed conversation.
func BuildClaimNote(request ClaimRequest) string {
	return "Dex claim: assigned to " + request.AssigneeEmail + ".\n\n" + request.Note
}

// BuildClaimRequest validates Start Flow input so no connector Step receives an unusable claim.
func BuildClaimRequest(input Input) (ClaimRequest, error) {
	request := ClaimRequest{
		InboxEmail: strings.ToLower(strings.TrimSpace(input.InboxEmail)), AssigneeEmail: strings.TrimSpace(input.AssigneeEmail),
		ClaimTagName: strings.TrimSpace(input.ClaimTagName), Note: strings.TrimSpace(input.Note), ReplyDraft: strings.TrimSpace(input.ReplyDraft),
	}
	for field, value := range map[string]string{"inboxEmail": request.InboxEmail, "assigneeEmail": request.AssigneeEmail} {
		address, err := mail.ParseAddress(value)
		if err != nil || address.Name != "" || !strings.EqualFold(address.Address, value) {
			return ClaimRequest{}, fmt.Errorf("%s %q must be one bare email address", field, value)
		}
	}
	switch {
	case request.Note == "" || len(request.Note) > maximumNoteBytes:
		return ClaimRequest{}, fmt.Errorf("note is required and at most %d bytes", maximumNoteBytes)
	case len(request.ReplyDraft) > maximumDraftBytes:
		return ClaimRequest{}, fmt.Errorf("replyDraft is at most %d bytes", maximumDraftBytes)
	case len(request.ClaimTagName) > 100 || strings.ContainsAny(request.ClaimTagName, "\r\n"):
		return ClaimRequest{}, errors.New("claimTagName is one Hiver tag name of at most 100 bytes")
	}
	return request, nil
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Validate the claim and record it before calling Hiver."
type recordClaimRequest struct {
	dex.StepDefaults
}

func (recordClaimRequest) GetStepType() string { return recordClaimRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordClaimRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordClaimRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	request, err := BuildClaimRequest(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := claimRequestAttribute.Set(ctx, request); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[InboxSearch](findSharedInboxStepType), InboxSearch{}), nil
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Pick the inbox with the requested address, read the next page, or fail after the last page."
type chooseSharedInbox struct {
	dex.StepDefaultsNoWaitFor[hiver.ListInboxesResult]
}

func (chooseSharedInbox) GetStepType() string { return chooseSharedInboxStepType }

func (chooseSharedInbox) Execute(ctx dex.Context, result hiver.ListInboxesResult) (*dex.StepDecision, error) {
	request, err := claimRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// The first inbox page finds no outcome yet; a later page continues its page count.
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if isAttributeNotFound(err) {
		outcome, err = ClaimOutcome{}, nil
	}
	if err != nil {
		return nil, err
	}
	outcome.InboxPagesRead++
	inbox, isFound := FindInboxByEmail(result.Value.Inboxes, request.InboxEmail)
	if isFound {
		outcome.InboxID = inbox.ID
	}
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	switch {
	case isFound:
		return dex.GoTo(sdkgo.StepRef[ConversationSearch](findUnassignedConversationStepType), ConversationSearch{InboxID: inbox.ID}), nil
	case result.Value.NextPageToken != "" && outcome.InboxPagesRead < MaximumInboxPages:
		return dex.GoTo(sdkgo.StepRef[InboxSearch](findSharedInboxStepType), InboxSearch{PageToken: result.Value.NextPageToken}), nil
	default:
		return dex.ForceFail("no Hiver shared inbox among the pages read has the address " + request.InboxEmail), nil
	}
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Read the first open unassigned conversation, scan the next page, or complete with nothing to claim."
type chooseConversation struct {
	dex.StepDefaultsNoWaitFor[hiver.ListConversationsResult]
}

func (chooseConversation) GetStepType() string { return chooseConversationStepType }

func (chooseConversation) Execute(ctx dex.Context, result hiver.ListConversationsResult) (*dex.StepDecision, error) {
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.ConversationPagesRead++
	conversation, isFound := FindClaimableConversation(result.Value.Conversations)
	hasNextPage := result.Value.NextPageToken != "" && outcome.ConversationPagesRead < MaximumConversationPages
	if !isFound && !hasNextPage {
		outcome.Action = ClaimActionNothingToClaim
	}
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	switch {
	case isFound:
		return dex.GoTo(sdkgo.StepRef[ConversationReference](readConversationStepType),
			ConversationReference{InboxID: outcome.InboxID, ConversationID: conversation.ID}), nil
	case hasNextPage:
		return dex.GoTo(sdkgo.StepRef[ConversationSearch](findUnassignedConversationStepType),
			ConversationSearch{InboxID: outcome.InboxID, PageToken: result.Value.NextPageToken}), nil
	default:
		return dex.GracefulComplete(outcome), nil
	}
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Claim the conversation only if it is still open and unassigned as just read."
type confirmConversation struct {
	dex.StepDefaultsNoWaitFor[hiver.GetConversationResult]
}

func (confirmConversation) GetStepType() string { return confirmConversationStepType }

func (confirmConversation) Execute(ctx dex.Context, result hiver.GetConversationResult) (*dex.StepDecision, error) {
	request, err := claimRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	conversation := result.Value.Conversation
	if !IsClaimable(conversation) {
		outcome.Action, outcome.Conversation = ClaimActionClaimedElsewhere, &conversation
		if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
			return nil, err
		}
		return dex.GracefulComplete(outcome), nil
	}
	if messages := result.Value.Messages; len(messages) != 0 {
		outcome.ReplyToMessage = &messages[len(messages)-1]
	}
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ConversationClaim](claimConversationStepType), ConversationClaim{
		InboxID: outcome.InboxID, ConversationID: conversation.ID, AssigneeEmail: request.AssigneeEmail, ClaimTagName: request.ClaimTagName,
	}), nil
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Record the claimed conversation as read back and prepare the internal note."
type recordClaimedConversation struct {
	dex.StepDefaultsNoWaitFor[hiver.UpdateConversationResult]
}

func (recordClaimedConversation) GetStepType() string { return recordClaimedConversationStepType }

func (recordClaimedConversation) Execute(ctx dex.Context, result hiver.UpdateConversationResult) (*dex.StepDecision, error) {
	request, err := claimRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	conversation := result.Value.Conversation
	outcome.Action, outcome.Conversation = ClaimActionClaimed, &conversation
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ClaimNote](addClaimNoteStepType),
		ClaimNote{InboxID: outcome.InboxID, ConversationID: conversation.ID, Content: BuildClaimNote(request)}), nil
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Record the note, then draft the reply when one was requested and the conversation has a message."
type recordClaimNote struct {
	dex.StepDefaultsNoWaitFor[hiver.AddNoteResult]
}

func (recordClaimNote) GetStepType() string { return recordClaimNoteStepType }

func (recordClaimNote) Execute(ctx dex.Context, result hiver.AddNoteResult) (*dex.StepDecision, error) {
	request, err := claimRequestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NoteID = result.Value.Note.ID
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	if request.ReplyDraft == "" || outcome.ReplyToMessage == nil {
		return dex.GracefulComplete(outcome), nil
	}
	return dex.GoTo(sdkgo.StepRef[ReplyDraft](draftCustomerReplyStepType), ReplyDraft{
		InboxID: outcome.InboxID, HiverMessageID: outcome.ReplyToMessage.HiverMessageID,
		GmailMessageID: replyGmailMessageID(*outcome.ReplyToMessage), Body: request.ReplyDraft,
	}), nil
}

// replyGmailMessageID is used only when the message has no Hiver message ID, because the draft takes exactly one.
func replyGmailMessageID(message hiver.ConversationMessage) string {
	if message.HiverMessageID != "" {
		return ""
	}
	return message.GmailMessageID
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The note was sent with an unknown outcome; complete for a person to check Hiver instead of resending it."
type recordUncertainNote struct {
	dex.StepDefaultsNoWaitFor[hiver.AddNoteResult]
}

func (recordUncertainNote) GetStepType() string { return recordUncertainNoteStepType }

func (recordUncertainNote) Execute(ctx dex.Context, result hiver.AddNoteResult) (*dex.StepDecision, error) {
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, addClaimNoteReviewReason, failureMessage(result.Failure)
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:claim group-label:"Claim"
// dex:explanation text:"Record the shared reply draft and complete the Flow."
type completeClaim struct {
	dex.StepDefaultsNoWaitFor[hiver.CreateSharedDraftResult]
}

func (completeClaim) GetStepType() string { return completeClaimStepType }

func (completeClaim) Execute(ctx dex.Context, result hiver.CreateSharedDraftResult) (*dex.StepDecision, error) {
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.SharedDraftID = result.Value.ID
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The draft was sent with an unknown outcome; complete for a person to check Hiver instead of resending it."
type recordUncertainDraft struct {
	dex.StepDefaultsNoWaitFor[hiver.CreateSharedDraftResult]
}

func (recordUncertainDraft) GetStepType() string { return recordUncertainDraftStepType }

func (recordUncertainDraft) Execute(ctx dex.Context, result hiver.CreateSharedDraftResult) (*dex.StepDecision, error) {
	outcome, err := claimOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, draftCustomerReplyReviewReason, failureMessage(result.Failure)
	if err := claimOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

func claimInspection(ctx dex.Context) (ClaimRequest, ClaimOutcome, error) {
	request, err := optionalAttribute(ctx, claimRequestAttribute)
	if err != nil {
		return ClaimRequest{}, ClaimOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, claimOutcomeAttribute)
	if err != nil {
		return ClaimRequest{}, ClaimOutcome{}, err
	}
	return request, outcome, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	if isAttributeNotFound(err) {
		var zero T
		return zero, nil
	}
	return value, err
}

func isAttributeNotFound(err error) bool {
	var missingAttribute *dex.AttributeNotFoundError
	return errors.As(err, &missingAttribute)
}

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
