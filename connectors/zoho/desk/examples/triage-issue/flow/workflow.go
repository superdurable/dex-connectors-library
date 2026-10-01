// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package triageissue demonstrates every Zoho Desk operation in one Flow started from Dex Web
// Start Flow: find the contact's unresolved ticket in a department, or create one, set its
// priority, and add one private triage comment.
package triageissue

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zoho/desk"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ZohoDeskTriageIssue"
	// ConnectionName is the static Dex Web connection for Zoho Desk.
	ConnectionName = "zoho-desk-helpdesk"

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
	addTriageCommentStepType          = "AddTriageComment"
	completeTriageStepType            = "CompleteTriage"
	recordUncertainCommentStepType    = "RecordUncertainComment"

	// SearchPageLimit is the number of tickets one search page reads.
	SearchPageLimit           = 25
	candidateThreadLimit      = 3
	candidateCommentLimit     = 5
	maximumPriorityNameLength = 120

	openIssueTicketReviewReason  = "createTicket"
	addTriageCommentReviewReason = "addComment"
)

var (
	customerIssueAttribute = dex.DefineAttribute[CustomerIssue]("zoho-desk-customer-issue")
	triageOutcomeAttribute = dex.DefineAttribute[TriageOutcome]("zoho-desk-triage-outcome")

	zohoIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// UnresolvedTicketStatusTypes are Zoho Desk's status types of tickets that still need work, Open
// and On Hold, so an organization's custom statuses match without being listed.
var UnresolvedTicketStatusTypes = []desk.TicketStatusType{desk.TicketStatusTypeOpen, desk.TicketStatusTypeOnHold}

// Input is the customer issue entered in Dex Web Start Flow.
type Input struct {
	// ContactEmail is the customer's email address, such as jane@acme.example.com.
	ContactEmail string `json:"contactEmail"`
	// ContactFirstName names the contact Zoho Desk adds when the customer is new.
	ContactFirstName string `json:"contactFirstName,omitempty"`
	// ContactLastName names the contact Zoho Desk adds when the customer is new.
	ContactLastName string `json:"contactLastName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message: the description of a new ticket, or quoted in the
	// triage comment on an existing one.
	Message string `json:"message"`
	// DepartmentID is the Zoho Desk department that triages the issue, such as 1892000000006907.
	DepartmentID string `json:"departmentId"`
	// Priority is the Zoho Desk priority name to set, such as High, Medium, or Low.
	Priority desk.TicketPriority `json:"priority"`
}

// CustomerIssue is the validated issue every later Step reads.
type CustomerIssue struct {
	// ContactEmail is the customer's email address.
	ContactEmail string `json:"contactEmail"`
	// ContactFirstName names a new Zoho Desk contact.
	ContactFirstName string `json:"contactFirstName,omitempty"`
	// ContactLastName names a new Zoho Desk contact.
	ContactLastName string `json:"contactLastName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message.
	Message string `json:"message"`
	// DepartmentID is the department that triages the issue.
	DepartmentID string `json:"departmentId"`
	// Priority is the Zoho Desk priority to set.
	Priority desk.TicketPriority `json:"priority"`
}

// IssueTicketSearch is one page of the search for the contact's unresolved ticket.
type IssueTicketSearch struct {
	// ContactEmail is the customer's email address.
	ContactEmail string `json:"contactEmail"`
	// DepartmentID is the department to search.
	DepartmentID string `json:"departmentId"`
	// From is the zero-based index of the page's first result.
	From int `json:"from"`
}

// IssueTicketReference identifies an existing ticket the Flow reads.
type IssueTicketReference struct {
	// TicketID is the Zoho Desk ticket ID.
	TicketID string `json:"ticketId"`
}

// TicketPriorityChange is the confirmed ticket and the priority to set on it.
type TicketPriorityChange struct {
	// TicketID is the Zoho Desk ticket ID.
	TicketID string `json:"ticketId"`
	// Priority is the Zoho Desk priority to set.
	Priority desk.TicketPriority `json:"priority"`
}

// TriageComment is the private comment to add to the triaged ticket.
type TriageComment struct {
	// TicketID is the Zoho Desk ticket ID.
	TicketID string `json:"ticketId"`
	// Content is the plain-text comment.
	Content string `json:"content"`
}

// TriageAction is what the Flow did with the ticket.
type TriageAction string

const (
	// TriageTicketOpened means the contact had no confirmed unresolved ticket, so one was created.
	TriageTicketOpened TriageAction = "opened"
	// TriageTicketFollowedUp means the contact's unresolved ticket was reopened and reprioritized.
	TriageTicketFollowedUp TriageAction = "followedUp"
	// TriageTicketCreationUncertain means the create request was sent but its outcome is unknown.
	TriageTicketCreationUncertain TriageAction = "creationUncertain"
)

// TriageOutcome is the Flow result and the value of its outcome Attribute.
type TriageOutcome struct {
	// Action is what the Flow did with the ticket.
	Action TriageAction `json:"action"`
	// Ticket is the ticket Zoho Desk returned after the write.
	Ticket desk.Ticket `json:"ticket"`
	// WasAlreadyApplied reports that a repeated update found its values already applied.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// SkippedTicketID is a search match the Flow did not follow up on, because when read it was
	// closed, in another department, or another contact's.
	SkippedTicketID string `json:"skippedTicketId,omitempty"`
	// CommentID is the private triage comment Zoho Desk added.
	CommentID string `json:"commentId,omitempty"`
	// NeedsReview reports that a write was sent with an unknown outcome, so a person must check
	// Zoho Desk; the Flow never resends it.
	NeedsReview bool `json:"needsReview,omitempty"`
	// ReviewReason names the uncertain write: createTicket or addComment.
	ReviewReason string `json:"reviewReason,omitempty"`
	// ReviewDetail is the connector's credential-free explanation of why the outcome is unknown.
	ReviewDetail string `json:"reviewDetail,omitempty"`
}

// Flow triages one customer issue in Zoho Desk.
type Flow struct {
	dex.FlowDefaults
	connection desk.Connection
}

// NewFlow binds the Zoho Desk Connection at registration time.
func NewFlow(connection desk.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zoho Desk connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCustomerIssue{}),
		dex.DefineStep(desk.NewSearchTicketsStep(desk.SearchTicketsStepConfig[IssueTicketSearch]{
			StepType: findUnresolvedIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-desk", GroupLabel: "Zoho Desk",
				Explanation: "Search one page of the contact's open and on-hold tickets in the department.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchTicketsInput,
			Searched: sdkgo.GoTo(chooseIssueTicket{}),
		})),
		dex.DefineStep(chooseIssueTicket{}),
		dex.DefineStep(desk.NewGetTicketStep(desk.GetTicketStepConfig[IssueTicketReference]{
			StepType: readCandidateTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-desk", GroupLabel: "Zoho Desk",
				Explanation: "Read the candidate ticket with its contact and newest threads and comments before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(confirmCandidateTicket{}),
		})),
		dex.DefineStep(confirmCandidateTicket{}),
		dex.DefineStep(desk.NewUpdateTicketStep(desk.UpdateTicketStepConfig[TicketPriorityChange]{
			StepType: prioritizeIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-desk", GroupLabel: "Zoho Desk",
				Explanation: "Reopen the ticket and set its priority; a repeated attempt writes nothing twice.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateTicketInput,
			Updated: sdkgo.GoTo(recordPrioritizedTicket{}),
		})),
		dex.DefineStep(recordPrioritizedTicket{}),
		dex.DefineStep(desk.NewCreateTicketStep(desk.CreateTicketStepConfig[CustomerIssue]{
			StepType: openIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-desk", GroupLabel: "Zoho Desk",
				Explanation: "Open a ticket with the customer's message and priority, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateTicketInput,
			Created:   sdkgo.GoTo(recordOpenedTicket{}),
			Uncertain: sdkgo.GoTo(recordUncertainTicket{}),
		})),
		dex.DefineStep(recordOpenedTicket{}),
		dex.DefineStep(recordUncertainTicket{}),
		dex.DefineStep(desk.NewAddCommentStep(desk.AddCommentStepConfig[TriageComment]{
			StepType: addTriageCommentStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zoho-desk", GroupLabel: "Zoho Desk",
				Explanation: "Add one private triage comment, sending the request at most once.",
			},
			Connection: flow.connection, MapToOperationInput: MapToAddCommentInput,
			Added:     sdkgo.GoTo(completeTriage{}),
			Uncertain: sdkgo.GoTo(recordUncertainComment{}),
		})),
		dex.DefineStep(completeTriage{}),
		dex.DefineStep(recordUncertainComment{}),
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
// dex:field attribute-key:zoho-desk-customer-issue value-type:json editable:false description:"Customer issue"
// dex:field attribute-key:zoho-desk-triage-outcome value-type:json editable:false description:"Zoho Desk triage outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoho-desk-customer-issue": issue,
		"zoho-desk-triage-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the customer issue and the ticket and comment Zoho Desk returned.
//
// dex:field attribute-key:zoho-desk-customer-issue value-type:json editable:false description:"Contact, subject, message, department, and priority"
// dex:field attribute-key:zoho-desk-triage-outcome value-type:json editable:false description:"Action, ticket, comment, and review state"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := triageInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zoho-desk-customer-issue": issue,
		"zoho-desk-triage-outcome": outcome,
	}}, nil
}

// MapToSearchTicketsInput finds the contact's open and on-hold tickets in the department.
func MapToSearchTicketsInput(search IssueTicketSearch) desk.SearchTicketsInput {
	return desk.SearchTicketsInput{
		StatusTypes: UnresolvedTicketStatusTypes, ContactEmail: search.ContactEmail, DepartmentID: search.DepartmentID,
		From: search.From, Limit: SearchPageLimit,
	}
}

// MapToGetTicketInput reads the candidate ticket with its newest threads and comments.
func MapToGetTicketInput(reference IssueTicketReference) desk.GetTicketInput {
	return desk.GetTicketInput{TicketID: reference.TicketID, ThreadLimit: candidateThreadLimit, CommentLimit: candidateCommentLimit}
}

// MapToUpdateTicketInput reopens the ticket and sets its priority.
func MapToUpdateTicketInput(change TicketPriorityChange) desk.UpdateTicketInput {
	return desk.UpdateTicketInput{TicketID: change.TicketID, Status: desk.TicketStatusOpen, Priority: change.Priority}
}

// MapToCreateTicketInput opens a ticket whose description is the customer's message.
func MapToCreateTicketInput(issue CustomerIssue) desk.CreateTicketInput {
	return desk.CreateTicketInput{
		Subject: issue.Subject, Description: issue.Message, DepartmentID: issue.DepartmentID,
		Contact: desk.TicketContactInput{Email: issue.ContactEmail, FirstName: issue.ContactFirstName, LastName: issue.ContactLastName},
		Status:  desk.TicketStatusOpen, Priority: issue.Priority,
	}
}

// MapToAddCommentInput adds the triage comment as a private comment.
func MapToAddCommentInput(comment TriageComment) desk.AddCommentInput {
	return desk.AddCommentInput{TicketID: comment.TicketID, Content: comment.Content}
}

// ChooseNewestTicket returns the most recently modified ticket, or false when there is none.
func ChooseNewestTicket(tickets []desk.Ticket) (desk.Ticket, bool) {
	var chosen desk.Ticket
	isFound := false
	for _, ticket := range tickets {
		if !isFound || ticket.ModifiedAt.After(chosen.ModifiedAt) {
			chosen, isFound = ticket, true
		}
	}
	return chosen, isFound
}

// IsFollowUpCandidate reports whether the ticket, as just read, still belongs to the contact, is in
// the department, and is not closed. Zoho Desk indexes search results with a delay, so only a
// fresh read decides.
func IsFollowUpCandidate(details desk.TicketDetails, issue CustomerIssue) bool {
	if details.Ticket.IsClosed() || details.Ticket.DepartmentID != issue.DepartmentID {
		return false
	}
	if strings.EqualFold(details.Ticket.Email, issue.ContactEmail) {
		return true
	}
	return details.Contact != nil && strings.EqualFold(details.Contact.Email, issue.ContactEmail)
}

// BuildFollowUpComment is the private comment on an existing ticket the customer contacted us about again.
func BuildFollowUpComment(issue CustomerIssue) string {
	return "Dex triage: the customer contacted us again. Priority set to " + string(issue.Priority) +
		".\n\nCustomer message:\n" + issue.Message
}

// BuildOpenedTicketComment is the private comment on a ticket this Flow opened.
func BuildOpenedTicketComment(issue CustomerIssue) string {
	return "Dex triage: opened with priority " + string(issue.Priority) + "; the contact had no open or on-hold ticket in this department."
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
// dex:explanation text:"Validate the customer issue and record it before calling Zoho Desk."
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
		IssueTicketSearch{ContactEmail: issue.ContactEmail, DepartmentID: issue.DepartmentID}), nil
}

// BuildCustomerIssue validates Start Flow input so no connector Step receives an unusable issue.
func BuildCustomerIssue(input Input) (CustomerIssue, error) {
	issue := CustomerIssue{
		ContactEmail: strings.TrimSpace(input.ContactEmail), ContactFirstName: strings.TrimSpace(input.ContactFirstName),
		ContactLastName: strings.TrimSpace(input.ContactLastName), Subject: strings.TrimSpace(input.Subject),
		Message: strings.TrimSpace(input.Message), DepartmentID: strings.TrimSpace(input.DepartmentID),
		Priority: desk.TicketPriority(strings.TrimSpace(string(input.Priority))),
	}
	address, err := mail.ParseAddress(issue.ContactEmail)
	if err != nil || address.Name != "" || address.Address != issue.ContactEmail || strings.ContainsAny(issue.ContactEmail, ",*${}") {
		return CustomerIssue{}, fmt.Errorf("contactEmail %q must be one bare email address", input.ContactEmail)
	}
	if issue.Subject == "" || issue.Message == "" {
		return CustomerIssue{}, errors.New("subject and message are required")
	}
	if !zohoIDPattern.MatchString(issue.DepartmentID) {
		return CustomerIssue{}, errors.New("departmentId must be a numeric Zoho Desk department ID such as 1892000000006907")
	}
	if issue.Priority == "" || len(issue.Priority) > maximumPriorityNameLength || strings.ContainsAny(string(issue.Priority), ",*${}") {
		return CustomerIssue{}, errors.New("priority must be one Zoho Desk priority name, such as High, Medium, or Low")
	}
	return issue, nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Read the newest match, search the next page when this one has none, or open a ticket after the last page."
type chooseIssueTicket struct {
	dex.StepDefaultsNoWaitFor[desk.SearchTicketsResult]
}

func (chooseIssueTicket) GetStepType() string { return chooseIssueTicketStepType }

func (chooseIssueTicket) Execute(ctx dex.Context, result desk.SearchTicketsResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if ticket, isFound := ChooseNewestTicket(result.Value.Tickets); isFound {
		return dex.GoTo(sdkgo.StepRef[IssueTicketReference](readCandidateTicketStepType), IssueTicketReference{TicketID: ticket.ID}), nil
	}
	if result.Value.NextFrom > 0 {
		return dex.GoTo(sdkgo.StepRef[IssueTicketSearch](findUnresolvedIssueTicketStepType),
			IssueTicketSearch{ContactEmail: issue.ContactEmail, DepartmentID: issue.DepartmentID, From: result.Value.NextFrom}), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Follow up only on the contact's own unresolved ticket in the department; otherwise open a new ticket."
type confirmCandidateTicket struct {
	dex.StepDefaultsNoWaitFor[desk.GetTicketResult]
}

func (confirmCandidateTicket) GetStepType() string { return confirmCandidateTicketStepType }

func (confirmCandidateTicket) Execute(ctx dex.Context, result desk.GetTicketResult) (*dex.StepDecision, error) {
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
// dex:explanation text:"Record the reprioritized ticket and prepare its private triage comment."
type recordPrioritizedTicket struct {
	dex.StepDefaultsNoWaitFor[desk.UpdateTicketResult]
}

func (recordPrioritizedTicket) GetStepType() string { return recordPrioritizedTicketStepType }

func (recordPrioritizedTicket) Execute(ctx dex.Context, result desk.UpdateTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := TriageOutcome{Action: TriageTicketFollowedUp, Ticket: result.Value.Ticket, WasAlreadyApplied: result.Value.WasAlreadyApplied}
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[TriageComment](addTriageCommentStepType), TriageComment{TicketID: outcome.Ticket.ID, Content: BuildFollowUpComment(issue)}), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the opened ticket and prepare its private triage comment."
type recordOpenedTicket struct {
	dex.StepDefaultsNoWaitFor[desk.CreateTicketResult]
}

func (recordOpenedTicket) GetStepType() string { return recordOpenedTicketStepType }

func (recordOpenedTicket) Execute(ctx dex.Context, result desk.CreateTicketResult) (*dex.StepDecision, error) {
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
	return dex.GoTo(sdkgo.StepRef[TriageComment](addTriageCommentStepType), TriageComment{TicketID: outcome.Ticket.ID, Content: BuildOpenedTicketComment(issue)}), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The create request was sent with an unknown outcome; complete for a person to check Zoho Desk instead of resending it."
type recordUncertainTicket struct {
	dex.StepDefaultsNoWaitFor[desk.CreateTicketResult]
}

func (recordUncertainTicket) GetStepType() string { return recordUncertainTicketStepType }

func (recordUncertainTicket) Execute(ctx dex.Context, result desk.CreateTicketResult) (*dex.StepDecision, error) {
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
// dex:explanation text:"Record the private triage comment and complete the Flow."
type completeTriage struct {
	dex.StepDefaultsNoWaitFor[desk.AddCommentResult]
}

func (completeTriage) GetStepType() string { return completeTriageStepType }

func (completeTriage) Execute(ctx dex.Context, result desk.AddCommentResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.CommentID = result.Value.Comment.ID
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:review group-label:"Needs review"
// dex:explanation text:"The comment request was sent with an unknown outcome; complete for a person to check Zoho Desk instead of resending it."
type recordUncertainComment struct {
	dex.StepDefaultsNoWaitFor[desk.AddCommentResult]
}

func (recordUncertainComment) GetStepType() string { return recordUncertainCommentStepType }

func (recordUncertainComment) Execute(ctx dex.Context, result desk.AddCommentResult) (*dex.StepDecision, error) {
	outcome, err := triageOutcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome.NeedsReview, outcome.ReviewReason, outcome.ReviewDetail = true, addTriageCommentReviewReason, failureMessage(result.Failure)
	if err := triageOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
