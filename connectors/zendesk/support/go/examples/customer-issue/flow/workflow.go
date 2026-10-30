// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package customerissue demonstrates every Zendesk Support operation in one Flow started
// from Dex Web Start Flow: search the customer's unsolved tickets about an issue, read the
// newest one to confirm it still belongs to the customer and is unsolved, then add one
// internal follow-up note to it, or open a ticket when no such ticket exists.
package customerissue

import (
	"errors"
	"fmt"
	"net/mail"
	"regexp"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "ZendeskCustomerIssue"
	// ConnectionName is the static Dex Web connection for Zendesk Support.
	ConnectionName = "zendesk-support-desk"
	// RepeatContactTag marks a ticket that received a follow-up from this Flow.
	RepeatContactTag = "dex-repeat-contact"
	// FollowUpNote is the internal note added to the customer's unsolved ticket.
	FollowUpNote = "The customer contacted us again about this issue. Message recorded by Dex:"

	recordCustomerIssueStepType     = "RecordCustomerIssue"
	findUnsolvedIssueTicketStepType = "FindUnsolvedIssueTicket"
	chooseIssueTicketStepType       = "ChooseIssueTicket"
	readCandidateTicketStepType     = "ReadCandidateTicket"
	confirmCandidateTicketStepType  = "ConfirmCandidateTicket"
	addIssueFollowUpStepType        = "AddIssueFollowUp"
	completeFollowUpStepType        = "CompleteFollowUp"
	openIssueTicketStepType         = "OpenIssueTicket"
	completeOpenedTicketStepType    = "CompleteOpenedTicket"

	unsolvedTicketSearchPageSize = 10
	candidateCommentLimit        = 5
)

var (
	customerIssueAttribute = dex.DefineAttribute[CustomerIssue]("zendesk-customer-issue")
	issueOutcomeAttribute  = dex.DefineAttribute[CustomerIssueOutcome]("zendesk-customer-issue-outcome")

	issueTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_./-]{0,79}$`)
)

// Input is the customer issue entered in Dex Web Start Flow.
type Input struct {
	// RequesterEmail is the customer's email address, such as jane@acme.example.com.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName is used only when Zendesk creates a new user for the customer.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message: the first public comment of a new ticket, or quoted in
	// the internal follow-up note on an existing one.
	Message string `json:"message"`
	// IssueTag is one lowercase Zendesk tag that identifies the issue, such as billing-double-charge.
	IssueTag string `json:"issueTag"`
	// GroupID assigns a newly opened ticket to a Zendesk group; zero leaves routing to Zendesk.
	GroupID int64 `json:"groupId,omitempty"`
}

// CustomerIssue is the validated issue every later Step reads.
type CustomerIssue struct {
	// RequesterEmail is the customer's email address.
	RequesterEmail string `json:"requesterEmail"`
	// RequesterName is the customer's name for a new Zendesk user.
	RequesterName string `json:"requesterName,omitempty"`
	// Subject is the subject of a newly opened ticket.
	Subject string `json:"subject"`
	// Message is the customer's message.
	Message string `json:"message"`
	// IssueTag identifies the issue.
	IssueTag string `json:"issueTag"`
	// GroupID assigns a newly opened ticket, or zero.
	GroupID int64 `json:"groupId,omitempty"`
}

// IssueTicketReference identifies an existing ticket the Flow reads or updates.
type IssueTicketReference struct {
	// TicketID is the Zendesk ticket ID.
	TicketID int64 `json:"ticketId"`
}

// FollowUp is the confirmed ticket and the customer's message to note on it.
type FollowUp struct {
	// TicketID is the Zendesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Message is the customer's message.
	Message string `json:"message"`
}

// IssueAction is the Flow's terminal business outcome.
type IssueAction string

const (
	// IssueTicketOpened means the customer had no confirmed unsolved ticket, so one was created.
	IssueTicketOpened IssueAction = "opened"
	// IssueFollowUpAdded means an internal note was added to the customer's unsolved ticket.
	IssueFollowUpAdded IssueAction = "followedUp"
)

// CustomerIssueOutcome is the Flow result and the value of its outcome Attribute.
type CustomerIssueOutcome struct {
	// Action is what the Flow did.
	Action IssueAction `json:"action"`
	// Ticket is the ticket Zendesk returned after the write.
	Ticket support.Ticket `json:"ticket"`
	// WasAlreadyApplied reports that a repeated write Step found the ticket or note its earlier attempt wrote.
	WasAlreadyApplied bool `json:"wasAlreadyApplied,omitempty"`
	// SkippedTicketID is a search match the Flow did not follow up on because it was solved or
	// belonged to another requester when read.
	SkippedTicketID int64 `json:"skippedTicketId,omitempty"`
}

// Flow records one customer issue in Zendesk Support.
type Flow struct {
	dex.FlowDefaults
	connection support.Connection
}

// NewFlow binds the Zendesk Support Connection at registration time.
func NewFlow(connection support.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the application and Zendesk Support connector Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordCustomerIssue{}),
		dex.DefineStep(support.NewSearchTicketsStep(support.SearchTicketsStepConfig[CustomerIssue]{
			StepType: findUnsolvedIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zendesk", GroupLabel: "Zendesk Support",
				Explanation: "Search the customer's unsolved tickets that carry the issue tag.",
			},
			Connection: flow.connection, MapToOperationInput: MapToSearchTicketsInput,
			Searched: sdkgo.GoTo(chooseIssueTicket{}),
		})),
		dex.DefineStep(chooseIssueTicket{}),
		dex.DefineStep(support.NewGetTicketStep(support.GetTicketStepConfig[IssueTicketReference]{
			StepType: readCandidateTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zendesk", GroupLabel: "Zendesk Support",
				Explanation: "Read the candidate ticket with its requester and latest comments before writing to it.",
			},
			Connection: flow.connection, MapToOperationInput: MapToGetTicketInput,
			Found: sdkgo.GoTo(confirmCandidateTicket{}),
		})),
		dex.DefineStep(confirmCandidateTicket{}),
		dex.DefineStep(support.NewUpdateTicketStep(support.UpdateTicketStepConfig[FollowUp]{
			StepType: addIssueFollowUpStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zendesk", GroupLabel: "Zendesk Support",
				Explanation: "Add the follow-up as one internal note, reopen the ticket, and tag the repeat contact.",
			},
			Connection: flow.connection, MapToOperationInput: MapToUpdateTicketInput,
			Updated: sdkgo.GoTo(completeFollowUp{}),
		})),
		dex.DefineStep(completeFollowUp{}),
		dex.DefineStep(support.NewCreateTicketStep(support.CreateTicketStepConfig[CustomerIssue]{
			StepType: openIssueTicketStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "zendesk", GroupLabel: "Zendesk Support",
				Explanation: "Open a ticket with the customer's message under a Step-derived Idempotency-Key, so a retry returns the same ticket.",
			},
			Connection: flow.connection, MapToOperationInput: MapToCreateTicketInput,
			Created: sdkgo.GoTo(completeOpenedTicket{}),
		})),
		dex.DefineStep(completeOpenedTicket{}),
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
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerIssueAttribute, issueOutcomeAttribute}}
}

// GetDexSummary returns the customer issue and its outcome.
//
// dex:field attribute-key:zendesk-customer-issue value-type:json editable:false description:"Customer issue"
// dex:field attribute-key:zendesk-customer-issue-outcome value-type:json editable:false description:"Zendesk outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := customerIssueInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zendesk-customer-issue":         issue,
		"zendesk-customer-issue-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the customer issue and the ticket Zendesk returned.
//
// dex:field attribute-key:zendesk-customer-issue value-type:json editable:false description:"Requester, subject, message, and issue tag"
// dex:field attribute-key:zendesk-customer-issue-outcome value-type:json editable:false description:"Action and ticket"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	issue, outcome, err := customerIssueInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"zendesk-customer-issue":         issue,
		"zendesk-customer-issue-outcome": outcome,
	}}, nil
}

// MapToSearchTicketsInput finds the customer's unsolved tickets about the issue.
func MapToSearchTicketsInput(issue CustomerIssue) support.SearchTicketsInput {
	return support.SearchTicketsInput{
		Statuses:       []support.TicketStatus{support.TicketStatusNew, support.TicketStatusOpen, support.TicketStatusPending, support.TicketStatusHold},
		RequesterEmail: issue.RequesterEmail, Tags: []string{issue.IssueTag}, PageSize: unsolvedTicketSearchPageSize,
	}
}

// MapToGetTicketInput reads the candidate ticket with its latest comments.
func MapToGetTicketInput(reference IssueTicketReference) support.GetTicketInput {
	return support.GetTicketInput{TicketID: reference.TicketID, LatestCommentLimit: candidateCommentLimit}
}

// MapToUpdateTicketInput adds the follow-up as an internal note, reopens the ticket, and tags it.
func MapToUpdateTicketInput(followUp FollowUp) support.UpdateTicketInput {
	return support.UpdateTicketInput{
		TicketID: followUp.TicketID, Status: support.TicketStatusOpen, AddTags: []string{RepeatContactTag},
		Comment: &support.TicketCommentInput{Body: FollowUpNote + "\n\n" + followUp.Message, IsInternalNote: true},
	}
}

// MapToCreateTicketInput opens a ticket whose first public comment is the customer's message.
func MapToCreateTicketInput(issue CustomerIssue) support.CreateTicketInput {
	return support.CreateTicketInput{
		Subject: issue.Subject, Comment: support.TicketCommentInput{Body: issue.Message},
		Requester: &support.TicketRequesterInput{Email: issue.RequesterEmail, Name: issue.RequesterName},
		Priority:  support.TicketPriorityNormal, Tags: []string{issue.IssueTag}, GroupID: issue.GroupID,
	}
}

// ChooseNewestTicket returns the most recently updated ticket, or false when there is none.
func ChooseNewestTicket(tickets []support.Ticket) (support.Ticket, bool) {
	var chosen support.Ticket
	isFound := false
	for _, ticket := range tickets {
		if !isFound || ticket.UpdatedAt.After(chosen.UpdatedAt) {
			chosen, isFound = ticket, true
		}
	}
	return chosen, isFound
}

// IsFollowUpCandidate reports whether the ticket, as just read, still belongs to the customer,
// carries the issue tag, and is unsolved. Search results can lag a recent change by minutes,
// and Zendesk's requester search is not guaranteed to be an exact address match.
func IsFollowUpCandidate(details support.TicketDetails, issue CustomerIssue) bool {
	if details.Requester == nil || !strings.EqualFold(details.Requester.Email, issue.RequesterEmail) {
		return false
	}
	switch details.Ticket.Status {
	case support.TicketStatusSolved, support.TicketStatusClosed:
		return false
	}
	for _, tag := range details.Ticket.Tags {
		if tag == issue.IssueTag {
			return true
		}
	}
	return false
}

func customerIssueInspection(ctx dex.Context) (CustomerIssue, CustomerIssueOutcome, error) {
	issue, err := optionalAttribute(ctx, customerIssueAttribute)
	if err != nil {
		return CustomerIssue{}, CustomerIssueOutcome{}, err
	}
	outcome, err := optionalAttribute(ctx, issueOutcomeAttribute)
	if err != nil {
		return CustomerIssue{}, CustomerIssueOutcome{}, err
	}
	return issue, outcome, nil
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

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Validate the customer issue and record it before calling Zendesk."
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
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](findUnsolvedIssueTicketStepType), issue), nil
}

// BuildCustomerIssue validates Start Flow input so no connector Step receives an unusable issue.
func BuildCustomerIssue(input Input) (CustomerIssue, error) {
	issue := CustomerIssue{
		RequesterEmail: strings.TrimSpace(input.RequesterEmail), RequesterName: strings.TrimSpace(input.RequesterName),
		Subject: strings.TrimSpace(input.Subject), Message: strings.TrimSpace(input.Message),
		IssueTag: strings.TrimSpace(input.IssueTag), GroupID: input.GroupID,
	}
	address, err := mail.ParseAddress(issue.RequesterEmail)
	if err != nil || address.Name != "" || address.Address != issue.RequesterEmail {
		return CustomerIssue{}, fmt.Errorf("requesterEmail %q must be one bare email address", input.RequesterEmail)
	}
	if issue.Subject == "" || issue.Message == "" {
		return CustomerIssue{}, errors.New("subject and message are required")
	}
	if !issueTagPattern.MatchString(issue.IssueTag) {
		return CustomerIssue{}, errors.New("issueTag must be one lowercase Zendesk tag such as billing-double-charge")
	}
	if issue.GroupID < 0 {
		return CustomerIssue{}, errors.New("groupId cannot be negative")
	}
	return issue, nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Read the newest unsolved match before following up, or open a ticket when the search found none."
type chooseIssueTicket struct {
	dex.StepDefaultsNoWaitFor[support.SearchTicketsResult]
}

func (chooseIssueTicket) GetStepType() string { return chooseIssueTicketStepType }

func (chooseIssueTicket) Execute(ctx dex.Context, result support.SearchTicketsResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	ticket, isFound := ChooseNewestTicket(result.Value.Tickets)
	if isFound {
		return dex.GoTo(sdkgo.StepRef[IssueTicketReference](readCandidateTicketStepType), IssueTicketReference{TicketID: ticket.ID}), nil
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Follow up only on the customer's own unsolved ticket; otherwise open a new ticket."
type confirmCandidateTicket struct {
	dex.StepDefaultsNoWaitFor[support.GetTicketResult]
}

func (confirmCandidateTicket) GetStepType() string { return confirmCandidateTicketStepType }

func (confirmCandidateTicket) Execute(ctx dex.Context, result support.GetTicketResult) (*dex.StepDecision, error) {
	issue, err := customerIssueAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	if IsFollowUpCandidate(result.Value, issue) {
		return dex.GoTo(sdkgo.StepRef[FollowUp](addIssueFollowUpStepType), FollowUp{TicketID: result.Value.Ticket.ID, Message: issue.Message}), nil
	}
	if err := issueOutcomeAttribute.Set(ctx, CustomerIssueOutcome{SkippedTicketID: result.Value.Ticket.ID}); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[CustomerIssue](openIssueTicketStepType), issue), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the follow-up on the existing ticket as the outcome and complete the Flow."
type completeFollowUp struct {
	dex.StepDefaultsNoWaitFor[support.UpdateTicketResult]
}

func (completeFollowUp) GetStepType() string { return completeFollowUpStepType }

func (completeFollowUp) Execute(ctx dex.Context, result support.UpdateTicketResult) (*dex.StepDecision, error) {
	outcome := CustomerIssueOutcome{Action: IssueFollowUpAdded, Ticket: result.Value.Ticket, WasAlreadyApplied: result.Value.WasAlreadyApplied}
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:issue group-label:"Customer issue"
// dex:explanation text:"Record the opened ticket as the outcome and complete the Flow."
type completeOpenedTicket struct {
	dex.StepDefaultsNoWaitFor[support.CreateTicketResult]
}

func (completeOpenedTicket) GetStepType() string { return completeOpenedTicketStepType }

func (completeOpenedTicket) Execute(ctx dex.Context, result support.CreateTicketResult) (*dex.StepDecision, error) {
	// confirmCandidateTicket records a skipped match only when it routed here; a missing value means none.
	previous, err := issueOutcomeAttribute.Get(ctx)
	var missingOutcome *dex.AttributeNotFoundError
	if err != nil && !errors.As(err, &missingOutcome) {
		return nil, err
	}
	outcome := CustomerIssueOutcome{
		Action: IssueTicketOpened, Ticket: result.Value.Ticket, WasAlreadyApplied: result.Value.WasIdempotentReplay,
		SkippedTicketID: previous.SkippedTicketID,
	}
	if err := issueOutcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
