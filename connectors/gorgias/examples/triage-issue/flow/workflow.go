// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triageissue demonstrates every Gorgias operation in one Flow started from Dex Web
// Start Flow: find the customer, follow up on their open ticket about an issue or create one,
// set its priority, add one internal triage note, and optionally acknowledge the customer.
package triageissue

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/gorgias"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GorgiasTriageIssue"
	// ConnectionName is the static Dex Web connection for Gorgias.
	ConnectionName = "gorgias-helpdesk"
	// RepeatContactTag marks an existing ticket this Flow followed up on.
	RepeatContactTag = "dex-repeat-contact"
	// MaxSearchPages bounds how many pages of the customer's tickets the Flow reads.
	MaxSearchPages = 5

	recordCustomerIssueStepType           = "RecordCustomerIssue"
	findRequesterStepType                 = "FindRequester"
	chooseRequesterRouteStepType          = "ChooseRequesterRoute"
	findOpenIssueTicketStepType           = "FindOpenIssueTicket"
	chooseIssueTicketStepType             = "ChooseIssueTicket"
	readCandidateTicketStepType           = "ReadCandidateTicket"
	confirmCandidateTicketStepType        = "ConfirmCandidateTicket"
	prioritizeIssueTicketStepType         = "PrioritizeIssueTicket"
	recordPrioritizedTicketStepType       = "RecordPrioritizedTicket"
	openIssueTicketStepType               = "OpenIssueTicket"
	recordOpenedTicketStepType            = "RecordOpenedTicket"
	recordUncertainTicketStepType         = "RecordUncertainTicket"
	addTriageNoteStepType                 = "AddTriageNote"
	recordTriageNoteStepType              = "RecordTriageNote"
	recordUncertainNoteStepType           = "RecordUncertainNote"
	acknowledgeCustomerStepType           = "AcknowledgeCustomer"
	completeTriageStepType                = "CompleteTriage"
	recordUncertainAcknowledgmentStepType = "RecordUncertainAcknowledgment"

	candidateMessageLimit = 5

	openIssueTicketReviewReason     = "createTicket"
	addTriageNoteReviewReason       = "addNote"
	acknowledgeCustomerReviewReason = "addNote:publicReply"
)

var (
	customerIssueAttribute = dex.DefineAttribute[CustomerIssue]("gorgias-customer-issue")
	triageOutcomeAttribute = dex.DefineAttribute[TriageOutcome]("gorgias-triage-outcome")
	searchStateAttribute   = dex.DefineAttribute[IssueTicketSearch]("gorgias-ticket-search")

	issueTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,62}$`)
)

// Input is the customer issue entered in Dex Web Start Flow.
type Input struct {
	// RequesterEmail is the customer's email address, such as jane@acme.example.com.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names the customer Gorgias adds when the customer is new.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message: the first message of a new ticket, or quoted in the
	// triage note on an existing one.
	Message string `json:"message"`
	// IssueTag is one lowercase Gorgias tag that identifies the issue, such as billing-double-charge.
	IssueTag string `json:"issueTag"`
	// Priority is Gorgias's priority to set: low, normal, high, or critical.
	Priority gorgias.TicketPriority `json:"priority"`
	// Acknowledgement is an optional public email reply sent when the Flow follows up on the
	// customer's existing ticket; blank sends none.
	Acknowledgement string `json:"acknowledgement,omitempty"`
}

// CustomerIssue is the validated issue every later Step reads.
type CustomerIssue struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName names a new Gorgias customer.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message.
	Message string `json:"message"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Priority is the Gorgias priority to set.
	Priority gorgias.TicketPriority `json:"priority"`
	// Acknowledgement is the public reply for a followed-up ticket, or empty.
	Acknowledgement string `json:"acknowledgement,omitempty"`
}

// IssueTicketSearch is one page of the search for the customer's open issue ticket.
type IssueTicketSearch struct {
	// RequesterID is the Gorgias customer ID.
	RequesterID int64 `json:"requesterId"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// Cursor is the page to read, or empty for the first.
	Cursor string `json:"cursor,omitempty"`
	// PageNumber counts pages read so far, from 1.
	PageNumber int `json:"pageNumber"`
}

// IssueTicketReference identifies an existing ticket the Flow reads.
type IssueTicketReference struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
}

// TicketPriorityChange is the confirmed ticket and the priority to set on it.
type TicketPriorityChange struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
	// Priority is the Gorgias priority to set.
	Priority gorgias.TicketPriority `json:"priority"`
}

// TicketNote is a note or reply to add to the triaged ticket.
type TicketNote struct {
	// TicketID is the Gorgias ticket ID.
	TicketID int64 `json:"ticketId"`
	// Body is the plain-text note or reply.
	Body string `json:"body"`
}

// TriageAction is what the Flow did with the ticket.
type TriageAction string

const (
	// TriageTicketOpened means the customer had no confirmed open ticket for the issue, so one was created.
	TriageTicketOpened TriageAction = "opened"
	// TriageTicketFollowedUp means the customer's open ticket was reprioritized and tagged.
	TriageTicketFollowedUp TriageAction = "followedUp"
	// TriageTicketCreationUncertain means the create request was sent but Gorgias did not confirm a ticket for it.
	TriageTicketCreationUncertain TriageAction = "creationUncertain"
)

// TriageOutcome is the Flow result and the value of its outcome Attribute.
type TriageOutcome struct {
	// Action is what the Flow did with the ticket.
	Action TriageAction `json:"action"`
	// Ticket is the ticket Gorgias returned after the write.
	Ticket gorgias.Ticket `json:"ticket"`
	// WasAlreadyApplied reports that a repeated update found its values already applied.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// WasTicketCreatedByEarlierAttempt reports that a retried create found the ticket an earlier attempt created.
	WasTicketCreatedByEarlierAttempt bool `json:"wasTicketCreatedByEarlierAttempt,omitempty"`
	// SkippedTicketID is a listed ticket the Flow did not follow up on, because when read it was
	// closed, untagged, or another customer's.
	SkippedTicketID int64 `json:"skippedTicketId,omitempty"`
	// NoteID is the internal triage note Gorgias added.
	NoteID int64 `json:"noteId,omitempty"`
	// AcknowledgementID is the public reply Gorgias added; Gorgias emails it asynchronously.
	AcknowledgementID int64 `json:"acknowledgementId,omitempty"`
	// NeedsReview reports that a write was sent with an unknown outcome, so a person must check
	// Gorgias; the Flow never resends it.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the uncertain write: createTicket, addNote, or addNote:publicReply.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow triages one customer issue in Gorgias.
type Flow struct {
	dex.FlowDefaults
	connection gorgias.Connection
}

// NewFlow binds the Gorgias Connection at registration time.
func NewFlow(connection gorgias.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Gorgias connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCustomerIssue{}),
		dex.DefineStep(gorgias.NewFindCustomerByEmailStep(gorgias.FindCustomerByEmailStepConfig[CustomerIssue]{
			StepType: findRequesterStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Find the Gorgias customer whose primary email address is the requester's.",
			},
			Connection: flow.connection, MapToOperationInput: MapToFindCustomerByEmailInput,
			Found: sdkgo.GoTo(chooseRequesterRoute{}), NotFound: sdkgo.GoTo(chooseRequesterRoute{}),
		})),
		dex.DefineStep(chooseRequesterRoute{}),
		dex.DefineStep(gorgias.NewSearchTicketsStep(gorgias.SearchTicketsStepConfig[IssueTicketSearch]{
			StepType: findOpenIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "List one page of the customer's open tickets that carry the issue tag.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchTicketsInput,
			Searched: sdkgo.GoTo(chooseIssueTicket{}),
		})),
		dex.DefineStep(chooseIssueTicket{}),
		dex.DefineStep(gorgias.NewGetTicketStep(gorgias.GetTicketStepConfig[IssueTicketReference]{
			StepType: readCandidateTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Read the candidate ticket with its customer and latest messages before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(confirmCandidateTicket{}),
		})),
		dex.DefineStep(confirmCandidateTicket{}),
		dex.DefineStep(gorgias.NewUpdateTicketStep(gorgias.UpdateTicketStepConfig[TicketPriorityChange]{
			StepType: prioritizeIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Keep the ticket open, set its priority, and tag the repeat contact; a repeated attempt writes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateTicketInput,
			Updated: sdkgo.GoTo(recordPrioritizedTicket{}),
		})),
		dex.DefineStep(recordPrioritizedTicket{}),
		dex.DefineStep(gorgias.NewCreateTicketStep(gorgias.CreateTicketStepConfig[CustomerIssue]{
			StepType: openIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Open a ticket with the customer's message and priority; a retry looks an unconfirmed ticket up instead of resending.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateTicketInput,
			Created: sdkgo.GoTo(recordOpenedTicket{}), Uncertain: sdkgo.GoTo(recordUncertainTicket{}),
		})),
		dex.DefineStep(recordOpenedTicket{}),
		dex.DefineStep(recordUncertainTicket{}),
		dex.DefineStep(gorgias.NewAddNoteStep(gorgias.AddNoteStepConfig[TicketNote]{
			StepType: addTriageNoteStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Add one internal triage note; a retry looks an unconfirmed note up instead of resending.",
			},
			Connection: flow.connection, MapToOperationInput: MapToInternalNoteInput,
			Added: sdkgo.GoTo(recordTriageNote{}), Uncertain: sdkgo.GoTo(recordUncertainNote{}),
		})),
		dex.DefineStep(recordTriageNote{}),
		dex.DefineStep(recordUncertainNote{}),
		dex.DefineStep(gorgias.NewAddNoteStep(gorgias.AddNoteStepConfig[TicketNote]{
			StepType: acknowledgeCustomerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "gorgias", GroupLabel: "Gorgias",
				Explanation: "Email one public acknowledgement to the customer; a retry looks an unconfirmed reply up instead of resending.",
			},
			Connection: flow.connection, MapToOperationInput: MapToPublicReplyInput,
			Added: sdkgo.GoTo(completeTriage{}), Uncertain: sdkgo.GoTo(recordUncertainAcknowledgment{}),
		})),
		dex.DefineStep(completeTriage{}),
		dex.DefineStep(recordUncertainAcknowledgment{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the issue, search page, and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerIssueAttribute, searchStateAttribute, triageOutcomeAttribute}}
}

// GetDexSummary returns the customer issue and its triage outcome.
//
// dex:field attribute-key:gorgias-customer-issue value-type:json editable:false description:"Customer issue"
// dex:field attribute-key:gorgias-triage-outcome value-type:json editable:false description:"Gorgias triage outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gorgias-customer-issue": issue,
		"gorgias-triage-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the customer issue and the ticket, note, and reply Gorgias returned.
//
// dex:field attribute-key:gorgias-customer-issue value-type:json editable:false description:"Requester, subject, message, issue tag, priority, and acknowledgement"
// dex:field attribute-key:gorgias-triage-outcome value-type:json editable:false description:"Action, ticket, note, acknowledgement, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"gorgias-customer-issue": issue,
		"gorgias-triage-outcome": outcome,
	}}, nil
}

// MapToFindCustomerByEmailInput looks the customer up by the requester's email address.
func MapToFindCustomerByEmailInput(issue CustomerIssue) gorgias.FindCustomerByEmailInput {
	return gorgias.FindCustomerByEmailInput{Email: issue.RequesterEmail}
}

// MapToSearchTicketsInput lists the customer's open tickets that carry the issue tag.
func MapToSearchTicketsInput(search IssueTicketSearch) gorgias.SearchTicketsInput {
	return gorgias.SearchTicketsInput{
		RequesterID: search.RequesterID, Statuses: []gorgias.TicketStatus{gorgias.TicketStatusOpen},
		Tags: []string{search.IssueTag}, PageSize: gorgias.MaxSearchPageSize, Cursor: search.Cursor,
	}
}

// MapToGetTicketInput reads the candidate ticket with its latest messages.
func MapToGetTicketInput(reference IssueTicketReference) gorgias.GetTicketInput {
	return gorgias.GetTicketInput{TicketID: reference.TicketID, LatestMessageLimit: candidateMessageLimit}
}

// MapToUpdateTicketInput keeps the ticket open, sets its priority, and tags the repeat contact.
func MapToUpdateTicketInput(change TicketPriorityChange) gorgias.UpdateTicketInput {
	return gorgias.UpdateTicketInput{
		TicketID: change.TicketID, Status: gorgias.TicketStatusOpen, Priority: change.Priority, AddTags: []string{RepeatContactTag},
	}
}

// MapToCreateTicketInput opens a ticket whose first message is the customer's message.
func MapToCreateTicketInput(issue CustomerIssue) gorgias.CreateTicketInput {
	return gorgias.CreateTicketInput{
		Subject: issue.Subject, Description: issue.Message,
		Requester: gorgias.TicketRequesterInput{Email: issue.RequesterEmail, Name: issue.RequesterName},
		Status:    gorgias.TicketStatusOpen, Priority: issue.Priority, Tags: []string{issue.IssueTag},
	}
}

// MapToInternalNoteInput adds the triage note as an internal note.
func MapToInternalNoteInput(note TicketNote) gorgias.AddNoteInput {
	return gorgias.AddNoteInput{TicketID: note.TicketID, Body: note.Body}
}

// MapToPublicReplyInput emails the acknowledgement to the customer as a public reply.
func MapToPublicReplyInput(note TicketNote) gorgias.AddNoteInput {
	return gorgias.AddNoteInput{TicketID: note.TicketID, Body: note.Body, IsPublicReply: true}
}

// ChooseNewestTicket returns the most recently updated ticket, or false when there is none.
func ChooseNewestTicket(tickets []gorgias.Ticket) (gorgias.Ticket, bool) {
	var chosen gorgias.Ticket
	isFound := false
	for _, ticket := range tickets {
		if !isFound || ticket.UpdatedAt.After(chosen.UpdatedAt) {
			chosen, isFound = ticket, true
		}
	}
	return chosen, isFound
}

// IsFollowUpCandidate reports whether the ticket, as just read, still belongs to the customer,
// carries the issue tag, and is open. Only a fresh read decides, because the ticket may have
// changed since it was listed.
func IsFollowUpCandidate(details gorgias.TicketDetails, issue CustomerIssue) bool {
	if details.Requester == nil || !strings.EqualFold(details.Requester.Email, issue.RequesterEmail) {
		return false
	}
	return details.Ticket.Status == gorgias.TicketStatusOpen && slices.Contains(details.Ticket.Tags, issue.IssueTag)
}

// BuildFollowUpNote is the internal note on an existing ticket the customer contacted us about again.
func BuildFollowUpNote(issue CustomerIssue) string {
	return "Dex triage: the customer contacted us again about " + issue.IssueTag + ". Priority set to " +
		string(issue.Priority) + ".\n\nCustomer message:\n" + issue.Message
}

// BuildOpenedTicketNote is the internal note on a ticket this Flow opened.
func BuildOpenedTicketNote(issue CustomerIssue) string {
	return "Dex triage: opened for " + issue.IssueTag + " with priority " + string(issue.Priority) +
		"; the customer had no open ticket carrying the tag."
}

// BuildCustomerIssue validates Start Flow input so no connector Step receives an unusable issue.
func BuildCustomerIssue(input Input) (CustomerIssue, error) {
	issue := CustomerIssue{
		RequesterEmail: strings.TrimSpace(input.RequesterEmail), RequesterName: strings.TrimSpace(input.RequesterName),
		Subject: strings.TrimSpace(input.Subject), Message: strings.TrimSpace(input.Message),
		IssueTag: strings.TrimSpace(input.IssueTag), Priority: input.Priority, Acknowledgement: strings.TrimSpace(input.Acknowledgement),
	}
	address, err := mail.ParseAddress(issue.RequesterEmail)
	if err != nil || address.Name != "" || address.Address != issue.RequesterEmail {
		return CustomerIssue{}, fmt.Errorf("requesterEmail %q must be one bare email address", input.RequesterEmail)
	}
	if issue.Subject == "" || issue.Message == "" {
		return CustomerIssue{}, errors.New("subject and message are required")
	}
	if !issueTagPattern.MatchString(issue.IssueTag) {
		return CustomerIssue{}, errors.New("issueTag must be one lowercase Gorgias tag such as billing-double-charge")
	}
	if !slices.Contains(gorgias.TicketPriorities(), issue.Priority) {
		return CustomerIssue{}, errors.New("priority must be a Gorgias priority: low, normal, high, or critical")
	}
	return issue, nil
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
// dex:explanation text:"Validate the customer issue and record it before calling Gorgias."
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
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](findRequesterStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Search the known customer's open tickets, or open a ticket for a new customer."
type chooseRequesterRoute struct {
	dex.StepDefaultsNoWaitFor[gorgias.FindCustomerByEmailResult]
}

func (chooseRequesterRoute) GetStepType() string { return chooseRequesterRouteStepType }

func (chooseRequesterRoute) Execute(ctx dex.Context, result gorgias.FindCustomerByEmailResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if len(result.Value.Customers) == 0 {
		return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
	}
	search := IssueTicketSearch{RequesterID: result.Value.Customers[0].ID, IssueTag: issue.IssueTag, PageNumber: 1}
	if err := searchStateAttribute.Set(ctx, search); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[IssueTicketSearch](findOpenIssueTicketStepType), search), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Read the newest match, list the next page when this one has none, or open a ticket after the last page."
type chooseIssueTicket struct {
	dex.StepDefaultsNoWaitFor[gorgias.SearchTicketsResult]
}

func (chooseIssueTicket) GetStepType() string { return chooseIssueTicketStepType }

func (chooseIssueTicket) Execute(ctx dex.Context, result gorgias.SearchTicketsResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ticket, isFound := ChooseNewestTicket(result.Value.Tickets); isFound {
		return dex.GoTo(sdkgo.StepRef[IssueTicketReference](readCandidateTicketStepType), IssueTicketReference{TicketID: ticket.ID}), nil
	}
	search, err := searchStateAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if result.Value.NextCursor != "" && search.PageNumber < MaxSearchPages {
		next := IssueTicketSearch{RequesterID: search.RequesterID, IssueTag: issue.IssueTag, Cursor: result.Value.NextCursor, PageNumber: search.PageNumber + 1}
		if err := searchStateAttribute.Set(ctx, next); err != nil {
			return nil, err
		}
		return dex.GoTo(sdkgo.StepRef[IssueTicketSearch](findOpenIssueTicketStepType), next), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Follow up only on the customer's own open tagged ticket; otherwise open a new ticket."
type confirmCandidateTicket struct {
	dex.StepDefaultsNoWaitFor[gorgias.GetTicketResult]
}

func (confirmCandidateTicket) GetStepType() string { return confirmCandidateTicketStepType }

func (confirmCandidateTicket) Execute(ctx dex.Context, result gorgias.GetTicketResult) (*dex.StepDecision, error) {
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
// dex:explanation text:"Record the reprioritized ticket and prepare its internal triage note."
type recordPrioritizedTicket struct {
	dex.StepDefaultsNoWaitFor[gorgias.UpdateTicketResult]
}

func (recordPrioritizedTicket) GetStepType() string { return recordPrioritizedTicketStepType }

func (recordPrioritizedTicket) Execute(ctx dex.Context, result gorgias.UpdateTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := TriageOutcome{Action: TriageTicketFollowedUp, Ticket: result.Value.Ticket, WasAlreadyApplied: result.Value.WasAlreadyApplied}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TicketNote](addTriageNoteStepType), TicketNote{TicketID: outcome.Ticket.ID, Body: BuildFollowUpNote(issue)}), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the opened ticket and prepare its internal triage note."
type recordOpenedTicket struct {
	dex.StepDefaultsNoWaitFor[gorgias.CreateTicketResult]
}

func (recordOpenedTicket) GetStepType() string { return recordOpenedTicketStepType }

func (recordOpenedTicket) Execute(ctx dex.Context, result gorgias.CreateTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	// confirmCandidateTicket records an outcome only when it skipped a ticket; a missing value means none.
	previous, err := optionalAttribute(ctx, triageOutcomeAttribute)
	if err != nil {
		return nil, err
	}
	outcome := TriageOutcome{
		Action: TriageTicketOpened, Ticket: result.Value.Ticket, SkippedTicketID: previous.SkippedTicketID,
		WasTicketCreatedByEarlierAttempt: result.Value.WasCreatedByEarlierAttempt,
	}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TicketNote](addTriageNoteStepType), TicketNote{TicketID: outcome.Ticket.ID, Body: BuildOpenedTicketNote(issue)}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The create request was sent and Gorgias did not confirm a ticket for it; complete for a person to check Gorgias instead of resending it."
type recordUncertainTicket struct {
	dex.StepDefaultsNoWaitFor[gorgias.CreateTicketResult]
}

func (recordUncertainTicket) GetStepType() string { return recordUncertainTicketStepType }

func (recordUncertainTicket) Execute(ctx dex.Context, result gorgias.CreateTicketResult) (*dex.StepDecision, error) {
	previous, err := optionalAttribute(ctx, triageOutcomeAttribute)
	if err != nil {
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
// dex:explanation text:"Record the triage note, then acknowledge a followed-up customer when the issue carries an acknowledgement."
type recordTriageNote struct {
	dex.StepDefaultsNoWaitFor[gorgias.AddNoteResult]
}

func (recordTriageNote) GetStepType() string { return recordTriageNoteStepType }

func (recordTriageNote) Execute(ctx dex.Context, result gorgias.AddNoteResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NoteID = result.Value.Message.ID
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	if outcome.Action == TriageTicketFollowedUp && issue.Acknowledgement != "" {
		return dex.GoTo(sdkgo.StepRef[TicketNote](acknowledgeCustomerStepType), TicketNote{TicketID: outcome.Ticket.ID, Body: issue.Acknowledgement}), nil
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The note request was sent and Gorgias did not confirm a note for it; complete for a person to check Gorgias instead of resending it."
type recordUncertainNote struct {
	dex.StepDefaultsNoWaitFor[gorgias.AddNoteResult]
}

func (recordUncertainNote) GetStepType() string { return recordUncertainNoteStepType }

func (recordUncertainNote) Execute(ctx dex.Context, result gorgias.AddNoteResult) (*dex.StepDecision, error) {
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

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the public acknowledgement Gorgias will email and complete the Flow."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[gorgias.AddNoteResult]
}

func (completeTriage) GetStepType() string { return completeTriageStepType }

func (completeTriage) Execute(ctx dex.Context, result gorgias.AddNoteResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.AcknowledgementID = result.Value.Message.ID
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The reply request was sent and Gorgias did not confirm a reply for it; complete for a person to check Gorgias instead of emailing the customer twice."
type recordUncertainAcknowledgment struct {
	dex.StepDefaultsNoWaitFor[gorgias.AddNoteResult]
}

func (recordUncertainAcknowledgment) GetStepType() string {
	return recordUncertainAcknowledgmentStepType
}

func (recordUncertainAcknowledgment) Execute(ctx dex.Context, result gorgias.AddNoteResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, acknowledgeCustomerReviewReason, failureMessage(result.Failure)
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
