// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triageissue demonstrates every Freshdesk operation in one Flow started from Dex Web
// Start Flow: find the customer's unresolved ticket about an issue, or create one, set its
// priority, and add one private triage note.
package triageissue

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "FreshdeskTriageIssue"
	// ConnectionName is the static Dex Web connection for Freshdesk.
	ConnectionName = "freshdesk-helpdesk"
	// RepeatContactTag marks an existing ticket this Flow followed up on.
	RepeatContactTag = "dex-repeat-contact"

	recordCustomerIssueStepType       = "RecordCustomerIssue"
	findUnresolvedIssueTicketStepType = "FindUnresolvedIssueTicket"
	chooseIssueTicketStepType         = "ChooseIssueTicket"
	readCandidateTicketStepType       = "ReadCandidateTicket"
	confirmCandidateTicketStepType    = "ConfirmCandidateTicket"
	prioritizeIssueTicketStepType     = "PrioritizeIssueTicket"
	recordPrioritizedTicketStepType   = "RecordPrioritizedTicket"
	openIssueTicketStepType           = "OpenIssueTicket"
	recordOpenedTicketStepType        = "RecordOpenedTicket"
	recordUncertainTicketStepType     = "RecordUncertainTicket"
	addTriageNoteStepType             = "AddTriageNote"
	completeTriageStepType            = "CompleteTriage"
	recordUncertainNoteStepType       = "RecordUncertainNote"

	candidateConversationLimit = 5

	openIssueTicketReviewReason = "createTicket"
	addTriageNoteReviewReason   = "addNote"
)

var (
	customerIssueAttribute = dex.DefineAttribute[CustomerIssue]("freshdesk-customer-issue")
	triageOutcomeAttribute = dex.DefineAttribute[TriageOutcome]("freshdesk-triage-outcome")

	issueTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,62}$`)
)

// UnresolvedTicketStatuses is Freshdesk's documented "all unresolved tickets" filter: Open (2),
// Pending (3), and the custom statuses Waiting on Customer (6) and Waiting on Third Party (7).
// An account with other custom statuses lists them in GET /api/v2/ticket_fields.
var UnresolvedTicketStatuses = []freshdesk.TicketStatus{
	freshdesk.TicketStatusOpen, freshdesk.TicketStatusPending,
	freshdesk.TicketStatusWaitingOnCustomer, freshdesk.TicketStatusWaitingOnThirdParty,
}

// Input is the customer issue entered in Dex Web Start Flow.
type Input struct {
	// RequesterEmail is the customer's email address, such as jane@acme.example.com.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names the contact Freshdesk adds when the customer is new.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message: the description of a new ticket, or quoted in the
	// triage note on an existing one.
	Message string `json:"message"`
	// IssueTag is one lowercase Freshdesk tag that identifies the issue, such as billing-double-charge.
	IssueTag string `json:"issueTag"`
	// Priority is Freshdesk's integer priority to set: 1 (Low), 2 (Medium), 3 (High), or 4 (Urgent).
	Priority freshdesk.TicketPriority `json:"priority"`
	// GroupID assigns a newly opened ticket to a Freshdesk group; zero leaves routing to Freshdesk.
	GroupID int64 `json:"groupId,omitempty"`
}

// CustomerIssue is the validated issue every later Step reads.
type CustomerIssue struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names a new Freshdesk contact.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message.
	Message string `json:"message"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Priority is the Freshdesk priority to set.
	Priority freshdesk.TicketPriority `json:"priority"`
	// GroupID assigns a newly opened ticket, or zero.
	GroupID int64 `json:"groupId,omitempty"`
}

// IssueTicketSearch is one page of the search for the customer's unresolved issue ticket.
type IssueTicketSearch struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Page is the Freshdesk filter-query page to read.
	Page int `json:"page"`
}

// IssueTicketReference identifies an existing ticket the Flow reads.
type IssueTicketReference struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
}

// TicketPriorityChange is the confirmed ticket and the priority to set on it.
type TicketPriorityChange struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Priority is the Freshdesk priority to set.
	Priority freshdesk.TicketPriority `json:"priority"`
}

// TriageNote is the private note to add to the triaged ticket.
type TriageNote struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Body is the plain-text note.
	Body string `json:"body"`
}

// TriageAction is what the Flow did with the ticket.
type TriageAction string

const (
	// TriageTicketOpened means the customer had no confirmed unresolved ticket, so one was created.
	TriageTicketOpened TriageAction = "opened"
	// TriageTicketFollowedUp means the customer's unresolved ticket was reopened and reprioritized.
	TriageTicketFollowedUp TriageAction = "followedUp"
	// TriageTicketCreationUncertain means the create request was sent but its outcome is unknown.
	TriageTicketCreationUncertain TriageAction = "creationUncertain"
)

// TriageOutcome is the Flow result and the value of its outcome Attribute.
type TriageOutcome struct {
	// Action is what the Flow did with the ticket.
	Action TriageAction `json:"action"`
	// Ticket is the ticket Freshdesk returned after the write.
	Ticket freshdesk.Ticket `json:"ticket"`
	// WasAlreadyApplied reports that a repeated update found its values already applied.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// SkippedTicketID is a search match the Flow did not follow up on, because when read it was
	// resolved, untagged, or another requester's.
	SkippedTicketID int64 `json:"skippedTicketId,omitempty"`
	// NoteID is the private triage note Freshdesk added.
	NoteID int64 `json:"noteId,omitempty"`
	// NeedsReview reports that a write was sent with an unknown outcome, so a person must check
	// Freshdesk; the Flow never resends it.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the uncertain write: createTicket or addNote.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow triages one customer issue in Freshdesk.
type Flow struct {
	dex.FlowDefaults
	connection freshdesk.Connection
}

// NewFlow binds the Freshdesk Connection at registration time.
func NewFlow(connection freshdesk.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Freshdesk connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCustomerIssue{}),
		dex.DefineStep(freshdesk.NewSearchTicketsStep(freshdesk.SearchTicketsStepConfig[IssueTicketSearch]{
			StepType: findUnresolvedIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "freshdesk", GroupLabel: "Freshdesk",
				Explanation: "Search one page of the customer's unresolved tickets that carry the issue tag.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchTicketsInput,
			Searched: sdkgo.GoTo(chooseIssueTicket{}),
		})),
		dex.DefineStep(chooseIssueTicket{}),
		dex.DefineStep(freshdesk.NewGetTicketStep(freshdesk.GetTicketStepConfig[IssueTicketReference]{
			StepType: readCandidateTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "freshdesk", GroupLabel: "Freshdesk",
				Explanation: "Read the candidate ticket with its requester and first conversations before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(confirmCandidateTicket{}),
		})),
		dex.DefineStep(confirmCandidateTicket{}),
		dex.DefineStep(freshdesk.NewUpdateTicketStep(freshdesk.UpdateTicketStepConfig[TicketPriorityChange]{
			StepType: prioritizeIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "freshdesk", GroupLabel: "Freshdesk",
				Explanation: "Reopen the ticket, set its priority, and tag the repeat contact; a repeated attempt writes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateTicketInput,
			Updated: sdkgo.GoTo(recordPrioritizedTicket{}),
		})),
		dex.DefineStep(recordPrioritizedTicket{}),
		dex.DefineStep(freshdesk.NewCreateTicketStep(freshdesk.CreateTicketStepConfig[CustomerIssue]{
			StepType: openIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "freshdesk", GroupLabel: "Freshdesk",
				Explanation: "Open a ticket with the customer's message and priority, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateTicketInput,
			Created:   sdkgo.GoTo(recordOpenedTicket{}),
			Uncertain: sdkgo.GoTo(recordUncertainTicket{}),
		})),
		dex.DefineStep(recordOpenedTicket{}),
		dex.DefineStep(recordUncertainTicket{}),
		dex.DefineStep(freshdesk.NewAddNoteStep(freshdesk.AddNoteStepConfig[TriageNote]{
			StepType: addTriageNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "freshdesk", GroupLabel: "Freshdesk",
				Explanation: "Add one private triage note, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddNoteInput,
			Added:     sdkgo.GoTo(completeTriage{}),
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
// dex:field attribute-key:freshdesk-customer-issue value-type:json editable:false description:"Customer issue"
// dex:field attribute-key:freshdesk-triage-outcome value-type:json editable:false description:"Freshdesk triage outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"freshdesk-customer-issue": issue,
		"freshdesk-triage-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the customer issue and the ticket and note Freshdesk returned.
//
// dex:field attribute-key:freshdesk-customer-issue value-type:json editable:false description:"Requester, subject, message, issue tag, and priority"
// dex:field attribute-key:freshdesk-triage-outcome value-type:json editable:false description:"Action, ticket, note, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"freshdesk-customer-issue": issue,
		"freshdesk-triage-outcome": outcome,
	}}, nil
}

// MapToSearchTicketsInput finds the customer's unresolved tickets that carry the issue tag.
func MapToSearchTicketsInput(search IssueTicketSearch) freshdesk.SearchTicketsInput {
	return freshdesk.SearchTicketsInput{
		Statuses: UnresolvedTicketStatuses, Tags: []string{search.IssueTag},
		RequesterEmail: search.RequesterEmail, Page: search.Page,
	}
}

// MapToGetTicketInput reads the candidate ticket with its first conversations.
func MapToGetTicketInput(reference IssueTicketReference) freshdesk.GetTicketInput {
	return freshdesk.GetTicketInput{TicketID: reference.TicketID, ConversationLimit: candidateConversationLimit}
}

// MapToUpdateTicketInput reopens the ticket, sets its priority, and tags the repeat contact.
func MapToUpdateTicketInput(change TicketPriorityChange) freshdesk.UpdateTicketInput {
	return freshdesk.UpdateTicketInput{
		TicketID: change.TicketID, Status: freshdesk.TicketStatusOpen, Priority: change.Priority, AddTags: []string{RepeatContactTag},
	}
}

// MapToCreateTicketInput opens a ticket whose description is the customer's message.
func MapToCreateTicketInput(issue CustomerIssue) freshdesk.CreateTicketInput {
	return freshdesk.CreateTicketInput{
		Subject: issue.Subject, Description: issue.Message,
		Requester: freshdesk.TicketRequesterInput{Email: issue.RequesterEmail, Name: issue.RequesterName},
		Status:    freshdesk.TicketStatusOpen, Priority: issue.Priority, Tags: []string{issue.IssueTag}, GroupID: issue.GroupID,
	}
}

// MapToAddNoteInput adds the triage note as a private note.
func MapToAddNoteInput(note TriageNote) freshdesk.AddNoteInput {
	return freshdesk.AddNoteInput{TicketID: note.TicketID, Body: note.Body}
}

// ChooseNewestTicket returns the most recently updated ticket, or false when there is none.
func ChooseNewestTicket(tickets []freshdesk.Ticket) (freshdesk.Ticket, bool) {
	var chosen freshdesk.Ticket
	isFound := false
	for _, ticket := range tickets {
		if !isFound || ticket.UpdatedAt.After(chosen.UpdatedAt) {
			chosen, isFound = ticket, true
		}
	}
	return chosen, isFound
}

// IsFollowUpCandidate reports whether the ticket, as just read, still belongs to the customer,
// carries the issue tag, and is unresolved. Freshdesk indexes search results minutes late, and
// search pages may omit tags, so only a fresh read decides.
func IsFollowUpCandidate(details freshdesk.TicketDetails, issue CustomerIssue) bool {
	if details.Requester == nil || !strings.EqualFold(details.Requester.Email, issue.RequesterEmail) {
		return false
	}
	if !slices.Contains(UnresolvedTicketStatuses, details.Ticket.Status) {
		return false
	}
	for _, tag := range details.Ticket.Tags {
		if strings.EqualFold(tag, issue.IssueTag) {
			return true
		}
	}
	return false
}

// DescribeTicketPriority returns Freshdesk's label and integer, such as High (3), so a note shows both.
func DescribeTicketPriority(priority freshdesk.TicketPriority) string {
	labels := map[freshdesk.TicketPriority]string{
		freshdesk.TicketPriorityLow: "Low", freshdesk.TicketPriorityMedium: "Medium",
		freshdesk.TicketPriorityHigh: "High", freshdesk.TicketPriorityUrgent: "Urgent",
	}
	return fmt.Sprintf("%s (%d)", labels[priority], priority)
}

// BuildFollowUpNote is the private note on an existing ticket the customer contacted us about again.
func BuildFollowUpNote(issue CustomerIssue) string {
	return "Dex triage: the customer contacted us again about " + issue.IssueTag + ". Priority set to " +
		DescribeTicketPriority(issue.Priority) + ".\n\nCustomer message:\n" + issue.Message
}

// BuildOpenedTicketNote is the private note on a ticket this Flow opened.
func BuildOpenedTicketNote(issue CustomerIssue) string {
	return "Dex triage: opened for " + issue.IssueTag + " with priority " + DescribeTicketPriority(issue.Priority) +
		"; no unresolved ticket from this customer carried the tag."
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

func failureMessage(failure *sdkgo.Failure) string {
	if failure == nil {
		return ""
	}
	return failure.Message
}

func isAttributeNotFound(err error) bool {
	var missingAttribute *dex.AttributeNotFoundError
	return errors.As(err, &missingAttribute)
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Validate the customer issue and record it before calling Freshdesk."
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
	return dex.GoTo(sdkgo.StepRef[IssueTicketSearch](findUnresolvedIssueTicketStepType),
		IssueTicketSearch{RequesterEmail: issue.RequesterEmail, IssueTag: issue.IssueTag, Page: 1}), nil
}

// BuildCustomerIssue validates Start Flow input so no connector Step receives an unusable issue.
func BuildCustomerIssue(input Input) (CustomerIssue, error) {
	issue := CustomerIssue{
		RequesterEmail: strings.TrimSpace(input.RequesterEmail), RequesterName: strings.TrimSpace(input.RequesterName),
		Subject: strings.TrimSpace(input.Subject), Message: strings.TrimSpace(input.Message),
		IssueTag: strings.TrimSpace(input.IssueTag), Priority: input.Priority, GroupID: input.GroupID,
	}
	address, err := mail.ParseAddress(issue.RequesterEmail)
	if err != nil || address.Name != "" || address.Address != issue.RequesterEmail {
		return CustomerIssue{}, fmt.Errorf("requesterEmail %q must be one bare email address", input.RequesterEmail)
	}
	if issue.Subject == "" || issue.Message == "" {
		return CustomerIssue{}, errors.New("subject and message are required")
	}
	if !issueTagPattern.MatchString(issue.IssueTag) {
		return CustomerIssue{}, errors.New("issueTag must be one lowercase Freshdesk tag such as billing-double-charge")
	}
	if issue.Priority < freshdesk.TicketPriorityLow || issue.Priority > freshdesk.TicketPriorityUrgent {
		return CustomerIssue{}, errors.New("priority must be a Freshdesk priority: 1 (Low), 2 (Medium), 3 (High), or 4 (Urgent)")
	}
	if issue.GroupID < 0 {
		return CustomerIssue{}, errors.New("groupId cannot be negative")
	}
	return issue, nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Read the newest match, search the next page when this one has none, or open a ticket after the last page."
type chooseIssueTicket struct {
	dex.StepDefaultsNoWaitFor[freshdesk.SearchTicketsResult]
}

func (chooseIssueTicket) GetStepType() string { return chooseIssueTicketStepType }

func (chooseIssueTicket) Execute(ctx dex.Context, result freshdesk.SearchTicketsResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ticket, isFound := ChooseNewestTicket(result.Value.Tickets); isFound {
		return dex.GoTo(sdkgo.StepRef[IssueTicketReference](readCandidateTicketStepType), IssueTicketReference{TicketID: ticket.ID}), nil
	}
	if result.Value.NextPage > 0 {
		return dex.GoTo(sdkgo.StepRef[IssueTicketSearch](findUnresolvedIssueTicketStepType),
			IssueTicketSearch{RequesterEmail: issue.RequesterEmail, IssueTag: issue.IssueTag, Page: result.Value.NextPage}), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Follow up only on the customer's own unresolved tagged ticket; otherwise open a new ticket."
type confirmCandidateTicket struct {
	dex.StepDefaultsNoWaitFor[freshdesk.GetTicketResult]
}

func (confirmCandidateTicket) GetStepType() string { return confirmCandidateTicketStepType }

func (confirmCandidateTicket) Execute(ctx dex.Context, result freshdesk.GetTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if IsFollowUpCandidate(result.Value, issue) {
		return dex.GoTo(sdkgo.StepRef[TicketPriorityChange](prioritizeIssueTicketStepType),
			TicketPriorityChange{TicketID: result.Value.Ticket.ID, Priority: issue.Priority}), nil
	}
	if err := triageOutcomeAttribute.Set(ctx, TriageOutcome{SkippedTicketID: result.Value.Ticket.ID}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the reprioritized ticket and prepare its private triage note."
type recordPrioritizedTicket struct {
	dex.StepDefaultsNoWaitFor[freshdesk.UpdateTicketResult]
}

func (recordPrioritizedTicket) GetStepType() string { return recordPrioritizedTicketStepType }

func (recordPrioritizedTicket) Execute(ctx dex.Context, result freshdesk.UpdateTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := TriageOutcome{Action: TriageTicketFollowedUp, Ticket: result.Value.Ticket, WasAlreadyApplied: result.Value.WasAlreadyApplied}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageNote](addTriageNoteStepType), TriageNote{TicketID: outcome.Ticket.ID, Body: BuildFollowUpNote(issue)}), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the opened ticket and prepare its private triage note."
type recordOpenedTicket struct {
	dex.StepDefaultsNoWaitFor[freshdesk.CreateTicketResult]
}

func (recordOpenedTicket) GetStepType() string { return recordOpenedTicketStepType }

func (recordOpenedTicket) Execute(ctx dex.Context, result freshdesk.CreateTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// confirmCandidateTicket records an outcome only when it skipped a match; a missing value means none.
	previous, err := triageOutcomeAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	outcome := TriageOutcome{Action: TriageTicketOpened, Ticket: result.Value.Ticket, SkippedTicketID: previous.SkippedTicketID}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageNote](addTriageNoteStepType), TriageNote{TicketID: outcome.Ticket.ID, Body: BuildOpenedTicketNote(issue)}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The create request was sent with an unknown outcome; complete for a person to check Freshdesk instead of resending it."
type recordUncertainTicket struct {
	dex.StepDefaultsNoWaitFor[freshdesk.CreateTicketResult]
}

func (recordUncertainTicket) GetStepType() string { return recordUncertainTicketStepType }

func (recordUncertainTicket) Execute(ctx dex.Context, result freshdesk.CreateTicketResult) (*dex.StepDecision, error) {
	previous, err := triageOutcomeAttribute.Get(ctx)
	if err != nil && !isAttributeNotFound(err) {
		return nil, err
	}
	outcome := TriageOutcome{
		Action: TriageTicketCreationUncertain, SkippedTicketID: previous.SkippedTicketID, NeedsReview: true,
		ReviewReason: openIssueTicketReviewReason, ReviewDetail: failureMessage(result.Failure),
	}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the private triage note and complete the Flow."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[freshdesk.AddNoteResult]
}

func (completeTriage) GetStepType() string { return completeTriageStepType }

func (completeTriage) Execute(ctx dex.Context, result freshdesk.AddNoteResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NoteID = result.Value.Conversation.ID
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The note request was sent with an unknown outcome; complete for a person to check Freshdesk instead of resending it."
type recordUncertainNote struct {
	dex.StepDefaultsNoWaitFor[freshdesk.AddNoteResult]
}

func (recordUncertainNote) GetStepType() string { return recordUncertainNoteStepType }

func (recordUncertainNote) Execute(ctx dex.Context, result freshdesk.AddNoteResult) (*dex.StepDecision, error) {
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

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
