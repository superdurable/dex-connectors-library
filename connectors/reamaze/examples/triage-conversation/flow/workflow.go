// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triageconversation demonstrates every Re:amaze operation in one Flow started from Dex
// Web Start Flow: resolve the customer by email, find the customer's unresolved conversation
// about an issue or create one, reopen it, and add one internal triage note.
package triageconversation

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ReamazeTriageConversation"
	// ConnectionName is the static Dex Web connection for Re:amaze.
	ConnectionName = "reamaze-brand"
	// RepeatContactTag marks an existing conversation this Flow followed up on.
	RepeatContactTag = "dex-repeat-contact"
	// MaxCandidatePages bounds how many search pages the Flow reads before opening a conversation.
	MaxCandidatePages = 5

	recordCustomerIssueStepType             = "RecordCustomerIssue"
	findCustomerContactStepType             = "FindCustomerContact"
	routeCustomerContactStepType            = "RouteCustomerContact"
	findUnresolvedIssueConversationStepType = "FindUnresolvedIssueConversation"
	chooseIssueConversationStepType         = "ChooseIssueConversation"
	readCandidateConversationStepType       = "ReadCandidateConversation"
	confirmCandidateConversationStepType    = "ConfirmCandidateConversation"
	reopenIssueConversationStepType         = "ReopenIssueConversation"
	recordReopenedConversationStepType      = "RecordReopenedConversation"
	openIssueConversationStepType           = "OpenIssueConversation"
	recordOpenedConversationStepType        = "RecordOpenedConversation"
	recordUncertainConversationStepType     = "RecordUncertainConversation"
	addTriageNoteStepType                   = "AddTriageNote"
	completeTriageStepType                  = "CompleteTriage"
	recordUncertainNoteStepType             = "RecordUncertainNote"

	candidateMessageLimit = 5

	openIssueConversationReviewReason = "createConversation"
	addTriageNoteReviewReason         = "replyToConversation"
)

var (
	customerIssueAttribute = dex.DefineAttribute[CustomerIssue]("reamaze-customer-issue")
	triageOutcomeAttribute = dex.DefineAttribute[TriageOutcome]("reamaze-triage-outcome")

	issueTagPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,62}$`)
	channelSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
)

// UnresolvedConversationStatuses are the Re:amaze statuses this Flow follows up on: Open (0),
// Responded (1), On Hold (5), and AI Agent Assigned (7). Done, Spam, Archived, Auto-Done, and AI
// Agent Done conversations are resolved, so the Flow opens a new conversation instead.
var UnresolvedConversationStatuses = []reamaze.ConversationStatus{
	reamaze.ConversationStatusOpen, reamaze.ConversationStatusResponded,
	reamaze.ConversationStatusOnHold, reamaze.ConversationStatusAIAgentAssigned,
}

// Input is the customer issue entered in Dex Web Start Flow.
type Input struct {
	// RequesterEmail is the customer's email address, such as jane@acme.example.com.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names the contact Re:amaze adds when the customer is new.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened conversation.
	Subject string `json:"subject"`
	// Message is the customer's message: the first message of a new conversation, or quoted in
	// the triage note on an existing one.
	Message string `json:"message"`
	// IssueTag is one lowercase Re:amaze tag that identifies the issue, such as billing-double-charge.
	IssueTag string `json:"issueTag"`
	// Channel is the slug of the Re:amaze channel a new conversation is created in, such as support.
	Channel string `json:"channel"`
}

// CustomerIssue is the validated issue every later Step reads.
type CustomerIssue struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names a new Re:amaze contact.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened conversation.
	Subject string `json:"subject"`
	// Message is the customer's message.
	Message string `json:"message"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Channel is the channel slug for a new conversation.
	Channel string `json:"channel"`
}

// CustomerEmail is the address the Flow resolves to Re:amaze contacts.
type CustomerEmail struct {
	// Email is the customer's email address.
	Email string `json:"email"`
}

// IssueConversationSearch is one page of the search for the customer's unresolved issue conversation.
type IssueConversationSearch struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Page is the Re:amaze conversation page to read.
	Page int `json:"page"`
}

// IssueConversationReference identifies an existing conversation the Flow reads or reopens.
type IssueConversationReference struct {
	// ConversationID is the conversation's slug.
	ConversationID string `json:"conversationId"`
}

// TriageNote is the internal note to add to the triaged conversation.
type TriageNote struct {
	// ConversationID is the conversation's slug.
	ConversationID string `json:"conversationId"`
	// Text is the note.
	Text string `json:"text"`
}

// TriageAction is what the Flow did with the conversation.
type TriageAction string

const (
	// TriageConversationOpened means the customer had no confirmed unresolved conversation, so one was created.
	TriageConversationOpened TriageAction = "opened"
	// TriageConversationFollowedUp means the customer's unresolved conversation was reopened and tagged.
	TriageConversationFollowedUp TriageAction = "followedUp"
	// TriageConversationCreationUncertain means the create request was sent but its outcome is unknown.
	TriageConversationCreationUncertain TriageAction = "creationUncertain"
)

// TriageOutcome is the Flow result and the value of its outcome Attribute.
type TriageOutcome struct {
	// Action is what the Flow did with the conversation.
	Action TriageAction `json:"action"`
	// Conversation is the conversation Re:amaze returned after the write.
	Conversation reamaze.Conversation `json:"conversation"`
	// WasAlreadyApplied reports that the create or update found its own earlier write instead of writing again.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// IsKnownContact reports that Re:amaze already had a contact with the customer's email.
	IsKnownContact bool `json:"isKnownContact,omitempty"`
	// SkippedConversationID is a search match the Flow did not follow up on, because when read it
	// was resolved, untagged, or another requester's.
	SkippedConversationID string `json:"skippedConversationId,omitempty"`
	// NoteOriginID is the origin_id of the internal triage note Re:amaze added.
	NoteOriginID string `json:"noteOriginId,omitempty"`
	// NoteWasAlreadyApplied reports that a retried note found its own earlier message by origin_id.
	NoteWasAlreadyApplied bool `json:"noteWasAlreadyApplied,omitempty"`
	// NeedsReview reports that a write was sent with an unknown outcome, so a person must check
	// Re:amaze; the Flow never resends it.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the uncertain write: createConversation or replyToConversation.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow triages one customer issue in Re:amaze.
type Flow struct {
	dex.FlowDefaults
	connection reamaze.Connection
}

// NewFlow binds the Re:amaze Connection at registration time.
func NewFlow(connection reamaze.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Re:amaze connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCustomerIssue{}),
		dex.DefineStep(reamaze.NewFindContactByEmailStep(reamaze.FindContactByEmailStepConfig[CustomerEmail]{
			StepType: findCustomerContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "Resolve the customer's email to Re:amaze contacts; a new customer has no conversations to search.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindContactByEmailInput,
			Found:    sdkgo.GoTo(routeCustomerContact{}),
			NotFound: sdkgo.GoTo(routeCustomerContact{}),
		})),
		dex.DefineStep(routeCustomerContact{}),
		dex.DefineStep(reamaze.NewSearchConversationsStep(reamaze.SearchConversationsStepConfig[IssueConversationSearch]{
			StepType: findUnresolvedIssueConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "List one page of the customer's unarchived conversations that carry the issue tag, most recently changed first.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchConversationsInput,
			Searched: sdkgo.GoTo(chooseIssueConversation{}),
		})),
		dex.DefineStep(chooseIssueConversation{}),
		dex.DefineStep(reamaze.NewGetConversationStep(reamaze.GetConversationStepConfig[IssueConversationReference]{
			StepType: readCandidateConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "Read the candidate conversation with its newest messages before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetConversationInput,
			Found: sdkgo.GoTo(confirmCandidateConversation{}),
		})),
		dex.DefineStep(confirmCandidateConversation{}),
		dex.DefineStep(reamaze.NewUpdateConversationStep(reamaze.UpdateConversationStepConfig[IssueConversationReference]{
			StepType: reopenIssueConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "Reopen the conversation and tag the repeat contact; a repeated attempt writes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateConversationInput,
			Updated: sdkgo.GoTo(recordReopenedConversation{}),
		})),
		dex.DefineStep(recordReopenedConversation{}),
		dex.DefineStep(reamaze.NewCreateConversationStep(reamaze.CreateConversationStepConfig[CustomerIssue]{
			StepType: openIssueConversationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "Open a conversation with the customer's message, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateConversationInput,
			Created:   sdkgo.GoTo(recordOpenedConversation{}),
			Uncertain: sdkgo.GoTo(recordUncertainConversation{}),
		})),
		dex.DefineStep(recordOpenedConversation{}),
		dex.DefineStep(recordUncertainConversation{}),
		dex.DefineStep(reamaze.NewReplyToConversationStep(reamaze.ReplyToConversationStepConfig[TriageNote]{
			StepType: addTriageNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "reamaze", GroupLabel: "Re:amaze",
				Explanation: "Add one internal triage note, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToReplyToConversationInput,
			Replied:   sdkgo.GoTo(completeTriage{}),
			Uncertain: sdkgo.GoTo(recordUncertainNote{}),
		})),
		dex.DefineStep(completeTriage{}),
		dex.DefineStep(recordUncertainNote{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the issue and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerIssueAttribute, triageOutcomeAttribute}}
}

// GetDexSummary returns the customer issue and its triage outcome.
//
// dex:field attribute-key:reamaze-customer-issue value-type:json editable:false description:"Customer issue"
// dex:field attribute-key:reamaze-triage-outcome value-type:json editable:false description:"Re:amaze triage outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reamaze-customer-issue": issue,
		"reamaze-triage-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the customer issue and the conversation and note Re:amaze returned.
//
// dex:field attribute-key:reamaze-customer-issue value-type:json editable:false description:"Requester, subject, message, issue tag, and channel"
// dex:field attribute-key:reamaze-triage-outcome value-type:json editable:false description:"Action, conversation, note, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"reamaze-customer-issue": issue,
		"reamaze-triage-outcome": outcome,
	}}, nil
}

// MapToFindContactByEmailInput resolves the customer's email.
func MapToFindContactByEmailInput(customer CustomerEmail) reamaze.FindContactByEmailInput {
	return reamaze.FindContactByEmailInput{Email: customer.Email}
}

// MapToSearchConversationsInput lists the customer's unarchived conversations that carry the
// issue tag, most recently changed first.
func MapToSearchConversationsInput(search IssueConversationSearch) reamaze.SearchConversationsInput {
	return reamaze.SearchConversationsInput{
		RequesterEmail: search.RequesterEmail, Tags: []string{search.IssueTag},
		Sort: reamaze.ConversationSortChanged, Page: search.Page,
	}
}

// MapToGetConversationInput reads the candidate conversation with its newest messages.
func MapToGetConversationInput(reference IssueConversationReference) reamaze.GetConversationInput {
	return reamaze.GetConversationInput{ConversationID: reference.ConversationID, MessageLimit: candidateMessageLimit}
}

// MapToUpdateConversationInput reopens the conversation and tags the repeat contact.
func MapToUpdateConversationInput(reference IssueConversationReference) reamaze.UpdateConversationInput {
	status := reamaze.ConversationStatusOpen
	return reamaze.UpdateConversationInput{ConversationID: reference.ConversationID, Status: &status, AddTags: []string{RepeatContactTag}}
}

// MapToCreateConversationInput opens a conversation whose first message is the customer's message.
func MapToCreateConversationInput(issue CustomerIssue) reamaze.CreateConversationInput {
	return reamaze.CreateConversationInput{
		Subject: issue.Subject, Message: issue.Message, Channel: issue.Channel,
		Requester: reamaze.ConversationRequesterInput{Email: issue.RequesterEmail, Name: issue.RequesterName},
		Tags:      []string{issue.IssueTag},
	}
}

// MapToReplyToConversationInput adds the triage note as an internal note that leaves the status as set.
func MapToReplyToConversationInput(note TriageNote) reamaze.ReplyToConversationInput {
	return reamaze.ReplyToConversationInput{
		ConversationID: note.ConversationID, Text: note.Text, IsInternalNote: true, ShouldSuppressAutoResolve: true,
	}
}

// ChooseFollowUpCandidate returns the first conversation on the page, in Re:amaze's most recently
// changed order, that the customer started, carries the issue tag, and is unresolved.
func ChooseFollowUpCandidate(conversations []reamaze.Conversation, issue CustomerIssue) (reamaze.Conversation, bool) {
	for _, conversation := range conversations {
		if isFollowUpConversation(conversation, issue) {
			return conversation, true
		}
	}
	return reamaze.Conversation{}, false
}

// IsFollowUpCandidate reports whether the conversation, as just read, still belongs to the
// customer, carries the issue tag, and is unresolved. Re:amaze's requester filter also matches
// conversations the customer was copied on, so only the conversation's own author decides.
func IsFollowUpCandidate(details reamaze.ConversationDetails, issue CustomerIssue) bool {
	return isFollowUpConversation(details.Conversation, issue)
}

func isFollowUpConversation(conversation reamaze.Conversation, issue CustomerIssue) bool {
	if conversation.Requester == nil || !strings.EqualFold(conversation.Requester.Email, issue.RequesterEmail) {
		return false
	}
	if !slices.Contains(UnresolvedConversationStatuses, conversation.Status) {
		return false
	}
	return slices.ContainsFunc(conversation.Tags, func(tag string) bool { return strings.EqualFold(tag, issue.IssueTag) })
}

// DescribeConversationStatus returns Re:amaze's label and integer, such as Responded (1), so a note shows both.
func DescribeConversationStatus(status reamaze.ConversationStatus) string {
	labels := map[reamaze.ConversationStatus]string{
		reamaze.ConversationStatusOpen: "Open", reamaze.ConversationStatusResponded: "Responded",
		reamaze.ConversationStatusDone: "Done", reamaze.ConversationStatusSpam: "Spam",
		reamaze.ConversationStatusArchived: "Archived", reamaze.ConversationStatusOnHold: "On Hold",
		reamaze.ConversationStatusAutoDone: "Auto-Done", reamaze.ConversationStatusAIAgentAssigned: "AI Agent Assigned",
		reamaze.ConversationStatusAIAgentDone: "AI Agent Done", reamaze.ConversationStatusAISpam: "Spam (identified by AI)",
	}
	label, isKnown := labels[status]
	if !isKnown {
		label = "Unknown"
	}
	return fmt.Sprintf("%s (%d)", label, status)
}

// BuildFollowUpNote is the internal note on an existing conversation the customer contacted us about again.
func BuildFollowUpNote(issue CustomerIssue) string {
	return "Dex triage: the customer contacted us again about " + issue.IssueTag + ". Status set to " +
		DescribeConversationStatus(reamaze.ConversationStatusOpen) + ".\n\nCustomer message:\n" + issue.Message
}

// BuildOpenedConversationNote is the internal note on a conversation this Flow opened.
func BuildOpenedConversationNote(issue CustomerIssue) string {
	return "Dex triage: opened for " + issue.IssueTag + "; no unresolved conversation from this customer carried the tag."
}

func triageInspection(ctx dex.Context) (CustomerIssue, TriageOutcome, error) {
	issue, err := optionalAttribute(ctx, customerIssueAttribute)
	if err != nil {
		return CustomerIssue{}, TriageOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, triageOutcomeAttribute)
	if err != nil {
		return CustomerIssue{}, TriageOutcome{}, err
	}
	return issue, outcome, nil
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

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Validate the customer issue and record it before calling Re:amaze."
type recordCustomerIssue struct {
	dex.StepDefaults
}

func (recordCustomerIssue) GetStepType() string { return recordCustomerIssueStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordCustomerIssue) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordCustomerIssue) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	issue, err := BuildCustomerIssue(input)
	if err != nil {
		return dex.ForceFail(err.Error()), nil
	}
	if err := customerIssueAttribute.Set(ctx, issue); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerEmail](findCustomerContactStepType), CustomerEmail{Email: issue.RequesterEmail}), nil
}

// BuildCustomerIssue validates Start Flow input so no connector Step receives an unusable issue.
func BuildCustomerIssue(input Input) (CustomerIssue, error) {
	issue := CustomerIssue{
		RequesterEmail: strings.TrimSpace(input.RequesterEmail), RequesterName: strings.TrimSpace(input.RequesterName),
		Subject: strings.TrimSpace(input.Subject), Message: strings.TrimSpace(input.Message),
		IssueTag: strings.TrimSpace(input.IssueTag), Channel: strings.TrimSpace(input.Channel),
	}
	if !isBareEmailAddress(issue.RequesterEmail) {
		return CustomerIssue{}, fmt.Errorf("requesterEmail %q must be one bare email address", input.RequesterEmail)
	}
	if issue.Subject == "" || issue.Message == "" {
		return CustomerIssue{}, errors.New("subject and message are required")
	}
	if len(issue.Subject) > reamaze.MaxTextBytes || len(issue.Message) > reamaze.MaxTextBytes {
		return CustomerIssue{}, fmt.Errorf("subject and message must each be at most %d bytes", reamaze.MaxTextBytes)
	}
	if !issueTagPattern.MatchString(issue.IssueTag) {
		return CustomerIssue{}, errors.New("issueTag must be one lowercase Re:amaze tag such as billing-double-charge")
	}
	if !channelSlugPattern.MatchString(issue.Channel) {
		return CustomerIssue{}, errors.New("channel must be a lowercase Re:amaze channel slug such as support")
	}
	return issue, nil
}

func isBareEmailAddress(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Name == "" && address.Address == value && !strings.ContainsAny(value, " \"()<>,;:[]\\")
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Search a known customer's conversations; open a conversation for a customer Re:amaze does not know."
type routeCustomerContact struct {
	dex.StepDefaultsNoWaitFor[reamaze.FindContactByEmailResult]
}

func (routeCustomerContact) GetStepType() string { return routeCustomerContactStepType }

func (routeCustomerContact) Execute(ctx dex.Context, result reamaze.FindContactByEmailResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	isKnownContact := result.Branch == reamaze.FindContactByEmailBranchFound
	if err := triageOutcomeAttribute.Set(ctx, TriageOutcome{IsKnownContact: isKnownContact}); err != nil {
		return nil, err
	}
	// More candidate pages leave the customer possibly known, so the Flow searches rather than guess.
	if isKnownContact || result.Value.HasMoreCandidates {
		return dex.GoTo(sdkgo.StepRef[IssueConversationSearch](findUnresolvedIssueConversationStepType),
			IssueConversationSearch{RequesterEmail: issue.RequesterEmail, IssueTag: issue.IssueTag, Page: 1}), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueConversationStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Read the first follow-up candidate, search the next page when this one has none, or open a conversation."
type chooseIssueConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.SearchConversationsResult]
}

func (chooseIssueConversation) GetStepType() string { return chooseIssueConversationStepType }

func (chooseIssueConversation) Execute(ctx dex.Context, result reamaze.SearchConversationsResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if conversation, isFound := ChooseFollowUpCandidate(result.Value.Conversations, issue); isFound {
		return dex.GoTo(sdkgo.StepRef[IssueConversationReference](readCandidateConversationStepType),
			IssueConversationReference{ConversationID: conversation.ID}), nil
	}
	if result.Value.NextPage > 0 && result.Value.NextPage <= MaxCandidatePages {
		return dex.GoTo(sdkgo.StepRef[IssueConversationSearch](findUnresolvedIssueConversationStepType),
			IssueConversationSearch{RequesterEmail: issue.RequesterEmail, IssueTag: issue.IssueTag, Page: result.Value.NextPage}), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueConversationStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Follow up only on the customer's own unresolved tagged conversation; otherwise open a new conversation."
type confirmCandidateConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.GetConversationResult]
}

func (confirmCandidateConversation) GetStepType() string { return confirmCandidateConversationStepType }

func (confirmCandidateConversation) Execute(ctx dex.Context, result reamaze.GetConversationResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if IsFollowUpCandidate(result.Value, issue) {
		return dex.GoTo(sdkgo.StepRef[IssueConversationReference](reopenIssueConversationStepType),
			IssueConversationReference{ConversationID: result.Value.Conversation.ID}), nil
	}
	// routeCustomerContact records the outcome before any search, so it is present here.
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.SkippedConversationID = result.Value.Conversation.ID
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueConversationStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the reopened conversation and prepare its internal triage note."
type recordReopenedConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.UpdateConversationResult]
}

func (recordReopenedConversation) GetStepType() string { return recordReopenedConversationStepType }

func (recordReopenedConversation) Execute(ctx dex.Context, result reamaze.UpdateConversationResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.Conversation, outcome.WasAlreadyApplied = TriageConversationFollowedUp, result.Value.Conversation, result.Value.WasAlreadyApplied
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageNote](addTriageNoteStepType),
		TriageNote{ConversationID: outcome.Conversation.ID, Text: BuildFollowUpNote(issue)}), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the opened conversation and prepare its internal triage note."
type recordOpenedConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.CreateConversationResult]
}

func (recordOpenedConversation) GetStepType() string { return recordOpenedConversationStepType }

func (recordOpenedConversation) Execute(ctx dex.Context, result reamaze.CreateConversationResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.Conversation, outcome.WasAlreadyApplied = TriageConversationOpened, result.Value.Conversation, result.Value.WasAlreadyApplied
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageNote](addTriageNoteStepType),
		TriageNote{ConversationID: outcome.Conversation.ID, Text: BuildOpenedConversationNote(issue)}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The create request was sent with an unknown outcome and no conversation carries its key; complete for a person to check Re:amaze."
type recordUncertainConversation struct {
	dex.StepDefaultsNoWaitFor[reamaze.CreateConversationResult]
}

func (recordUncertainConversation) GetStepType() string { return recordUncertainConversationStepType }

func (recordUncertainConversation) Execute(ctx dex.Context, result reamaze.CreateConversationResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.Action, outcome.NeedsReview = TriageConversationCreationUncertain, true
	outcome.ReviewReason, outcome.ReviewDetail = openIssueConversationReviewReason, failureMessage(result.Failure)
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the internal triage note and complete the Flow."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[reamaze.ReplyToConversationResult]
}

func (completeTriage) GetStepType() string { return completeTriageStepType }

func (completeTriage) Execute(ctx dex.Context, result reamaze.ReplyToConversationResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NoteOriginID, outcome.NoteWasAlreadyApplied = result.Value.Message.OriginID, result.Value.WasAlreadyApplied
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The note request was sent with an unknown outcome and no message carries its origin_id; complete for a person to check Re:amaze."
type recordUncertainNote struct {
	dex.StepDefaultsNoWaitFor[reamaze.ReplyToConversationResult]
}

func (recordUncertainNote) GetStepType() string { return recordUncertainNoteStepType }

func (recordUncertainNote) Execute(ctx dex.Context, result reamaze.ReplyToConversationResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, addTriageNoteReviewReason, failureMessage(result.Failure)
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
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
