// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gorgias

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createTicketOperation = "createTicket"

var createTicketDispatchBranches = singleDispatchBranches{
	providerRejected: CreateTicketBranchProviderRejected, defect: CreateTicketBranchDefect,
}

// CreateTicketInput is one new ticket and the customer's first message. Zero values leave a
// field to Gorgias's defaults and the account's rules.
type CreateTicketInput struct {
	// Subject is the ticket subject, at most MaxSubjectLength characters. It is required.
	Subject string `json:"subject"`
	// Description is the customer's first message as plain text. It is required. The connector
	// also sends it HTML-escaped as body_html, so markup is shown literally and line breaks are kept.
	Description string `json:"description"`
	// Requester is the customer asking for help. It is required.
	Requester TicketRequesterInput `json:"requester"`
	// Status is open or closed, or empty for Gorgias's default, open.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is a Gorgias priority, or empty for Gorgias's default, normal.
	Priority TicketPriority `json:"priority,omitempty"`
	// Tags are tag names set on the new ticket; Gorgias creates unknown names and rules may add more.
	Tags []string `json:"tags,omitempty"`
	// AssigneeUserID assigns the ticket to a Gorgias user, or zero for none.
	AssigneeUserID int64 `json:"assigneeUserId,omitempty"`
	// AssigneeTeamID assigns the ticket to a Gorgias team, or zero for none.
	AssigneeTeamID int64 `json:"assigneeTeamId,omitempty"`
}

// TicketRequesterInput identifies the requester by email. Gorgias uses the customer with that
// primary address or, when none exists, adds a customer named Name.
type TicketRequesterInput struct {
	// Email is one bare address such as jane@example.com.
	Email string `json:"email"`
	// Name names a customer Gorgias adds; blank lets Gorgias derive one.
	Name string `json:"name,omitempty"`
}

// CreateTicketOutput is the created ticket.
type CreateTicketOutput struct {
	// Ticket is the ticket Gorgias created.
	Ticket Ticket `json:"ticket"`
	// WasCreatedByEarlierAttempt reports that an earlier attempt of this Step created the ticket
	// and this attempt found it by its external ID instead of sending the request again.
	WasCreatedByEarlierAttempt bool `json:"wasCreatedByEarlierAttempt,omitempty"`
}

// CreateTicketOperation is the createTicket Mutation.
type CreateTicketOperation struct {
	client *Client
}

type createTicketRequestWire struct {
	Customer     createTicketPersonWire    `json:"customer"`
	Subject      string                    `json:"subject"`
	Channel      string                    `json:"channel"`
	Via          string                    `json:"via"`
	FromAgent    bool                      `json:"from_agent"`
	Status       string                    `json:"status,omitempty"`
	Priority     string                    `json:"priority,omitempty"`
	Tags         []gorgiasTagWire          `json:"tags,omitempty"`
	AssigneeUser *gorgiasIDWire            `json:"assignee_user,omitempty"`
	AssigneeTeam *gorgiasIDWire            `json:"assignee_team,omitempty"`
	ExternalID   string                    `json:"external_id"`
	Messages     []createTicketMessageWire `json:"messages"`
}

type createTicketPersonWire struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

type createTicketMessageWire struct {
	Channel      string                 `json:"channel"`
	Via          string                 `json:"via"`
	FromAgent    bool                   `json:"from_agent"`
	Sender       createTicketPersonWire `json:"sender"`
	Subject      string                 `json:"subject"`
	BodyText     string                 `json:"body_text"`
	BodyHTML     string                 `json:"body_html"`
	StrippedText string                 `json:"stripped_text"`
}

// Definition returns the immutable connector operation definition.
func (CreateTicketOperation) Definition() sdkgo.MutationDefinition { return CreateTicketDefinition }

// IdempotencyKey prefixes the stable connector Call ID with dex-. Gorgias documents no idempotency
// key, so the connector writes this key to the ticket's external_id and finds the ticket by it.
func (CreateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey("dex-" + string(callID))
}

// Invoke sends POST /api/tickets once per Step execution unless Gorgias provably did not apply it.
// A 429 or a connection that never opened is retried and may send again, after a later attempt
// first looks the ticket up by external_id; any other unconfirmed outcome is retried only to look
// the ticket up, which selects created when found and uncertain when it stays missing.
func (operation CreateTicketOperation) Invoke(call sdkgo.Call, input CreateTicketInput) sdkgo.MutationAttempt[CreateTicketOutput] {
	if err := validateCreateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, gorgiasFailurePointer(sdkgo.FailureValidation, createTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	if hasEarlierDispatch(call) {
		credentials, failure := operation.client.resolveCredentials(call, createTicketOperation)
		if failure != nil {
			return reconcileCredentialFailureAttempt[CreateTicketOutput](call, *failure, createTicketOperation)
		}
		return operation.findEarlierTicket(call, credentials)
	}
	credentials, failure := operation.client.resolveCredentials(call, createTicketOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	if isLaterAttempt(call) {
		ticket, isFound, result, receipt := operation.lookUpTicketByExternalID(call, credentials)
		switch {
		case result.outcome != exchangeSucceeded:
			return unsentRequestAttempt[CreateTicketOutput](result, receipt, createTicketDispatchBranches)
		case isFound:
			return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{Ticket: ticket, WasCreatedByEarlierAttempt: true}, nil, receipt)
		}
	}
	if recordDispatch(call) != nil {
		return dispatchNotRecordedAttempt[CreateTicketOutput](createTicketOperation)
	}
	result := operation.client.exchange(call, credentials, createTicketOperation, gorgiasRequest{
		method: http.MethodPost, path: "/tickets", payload: buildCreateTicketRequest(input, string(call.IdempotencyKey)),
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := singleDispatchAttemptForSend[CreateTicketOutput](call, result, receipt, createTicketDispatchBranches); isTerminal {
		return attempt
	}
	ticket, _, err := decodeTicketBody(result.response.body, 0)
	if err != nil {
		return sdkgo.NewMutationRetry[CreateTicketOutput](gorgiasFailure(sdkgo.FailureProtocol, createTicketOperation,
			"Gorgias accepted the ticket but returned an invalid ticket: "+err.Error()), reconcileDelay)
	}
	return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{Ticket: ticket}, nil, operation.client.receipt(call, result.response, ticket.ID))
}

// findEarlierTicket looks up the ticket an earlier attempt may have created, by the external ID it wrote.
func (operation CreateTicketOperation) findEarlierTicket(call sdkgo.Call, credentials Credentials) sdkgo.MutationAttempt[CreateTicketOutput] {
	ticket, isFound, result, receipt := operation.lookUpTicketByExternalID(call, credentials)
	switch {
	case result.outcome != exchangeSucceeded:
		return reconcileLookupFailureAttempt[CreateTicketOutput](call, result, receipt, createTicketOperation)
	case isFound:
		return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{Ticket: ticket, WasCreatedByEarlierAttempt: true}, nil, receipt)
	default:
		return reconcileNotFoundAttempt[CreateTicketOutput](call, receipt, createTicketOperation, "ticket")
	}
}

// lookUpTicketByExternalID reads GET /api/tickets?external_id=; an unreadable page returns an exchangeInvalid result.
func (operation CreateTicketOperation) lookUpTicketByExternalID(call sdkgo.Call, credentials Credentials) (Ticket, bool, gorgiasExchange, sdkgo.Receipt) {
	externalID := string(call.IdempotencyKey)
	result := operation.client.exchange(call, credentials, createTicketOperation, gorgiasRequest{
		method: http.MethodGet, path: "/tickets", query: url.Values{"external_id": {externalID}, "limit": {"5"}},
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if result.outcome != exchangeSucceeded {
		return Ticket{}, false, result, receipt
	}
	wires, _, err := decodeListBody[gorgiasTicketWire](result.response.body, "ticket")
	if err != nil {
		return Ticket{}, false, gorgiasExchange{outcome: exchangeInvalid, response: result.response,
			failure: gorgiasFailure(sdkgo.FailureProtocol, createTicketOperation, "Gorgias returned an invalid ticket page: "+err.Error())}, receipt
	}
	for _, wire := range wires {
		ticket, err := decodeTicketWire(wire)
		if err == nil && ticket.ExternalID == externalID {
			return ticket, true, result, operation.client.receipt(call, result.response, ticket.ID)
		}
	}
	return Ticket{}, false, result, receipt
}

func validateCreateTicketInput(input CreateTicketInput) error {
	if err := validateTextInput("subject", input.Subject); err != nil {
		return err
	}
	if utf8.RuneCountInString(input.Subject) > MaxSubjectLength {
		return fmt.Errorf("subject is longer than Gorgias's %d-character limit", MaxSubjectLength)
	}
	if err := validateTextInput("description", input.Description); err != nil {
		return err
	}
	if !isBareEmailAddress(input.Requester.Email) {
		return errors.New("requester.email must be one bare email address such as jane@example.com")
	}
	if input.Requester.Name != strings.TrimSpace(input.Requester.Name) {
		return errors.New("requester.name cannot have surrounding spaces")
	}
	if input.Status != "" {
		if err := validateTicketStatus("status", input.Status); err != nil {
			return err
		}
	}
	if input.Priority != "" {
		if err := validateTicketPriority("priority", input.Priority); err != nil {
			return err
		}
	}
	return errors.Join(validateTags("tags", input.Tags), validateOptionalID("assigneeUserId", input.AssigneeUserID),
		validateOptionalID("assigneeTeamId", input.AssigneeTeamID))
}

// buildCreateTicketRequest sends the first message through the api channel, so Gorgias's auto-reply rules do not email the customer.
func buildCreateTicketRequest(input CreateTicketInput, externalID string) createTicketRequestWire {
	requester := createTicketPersonWire{Email: input.Requester.Email, Name: input.Requester.Name}
	request := createTicketRequestWire{
		Customer: requester, Subject: input.Subject, Channel: MessageChannelAPI, Via: MessageChannelAPI,
		Status: string(input.Status), Priority: string(input.Priority), ExternalID: externalID,
		Messages: []createTicketMessageWire{{
			Channel: MessageChannelAPI, Via: MessageChannelAPI, Sender: requester, Subject: input.Subject,
			BodyText: input.Description, BodyHTML: plainTextToGorgiasHTML(input.Description), StrippedText: input.Description,
		}},
	}
	for _, tag := range input.Tags {
		request.Tags = append(request.Tags, gorgiasTagWire{Name: tag})
	}
	if input.AssigneeUserID > 0 {
		request.AssigneeUser = &gorgiasIDWire{ID: input.AssigneeUserID}
	}
	if input.AssigneeTeamID > 0 {
		request.AssigneeTeam = &gorgiasIDWire{ID: input.AssigneeTeamID}
	}
	return request
}
