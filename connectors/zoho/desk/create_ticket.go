// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"errors"
	"net/http"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createTicketOperation = "createTicket"

// CreateTicketInput is one new ticket in a department. Zero values leave a field to Zoho Desk's
// defaults, the department's layout, and its automations.
type CreateTicketInput struct {
	// Subject is the ticket subject, at most 255 characters. It is required.
	Subject string `json:"subject"`
	// Description is the ticket's plain-text content, at most 65,535 characters. It is required.
	// The connector escapes it into Zoho Desk's HTML description, so markup is shown literally and
	// line breaks are kept.
	Description string `json:"description"`
	// DepartmentID is the department that owns the ticket. Zoho Desk requires it; GET
	// /api/v1/departments lists the organization's departments.
	DepartmentID string `json:"departmentId"`
	// ContactID names an existing contact. Set ContactID or Contact.Email, not both.
	ContactID string `json:"contactId,omitempty"`
	// Contact identifies the customer by email. Zoho Desk uses the contact with that address or,
	// when none exists, adds one with the given names.
	Contact TicketContactInput `json:"contact,omitzero"`
	// Status is a Zoho Desk status name, including custom statuses, or empty for the department's default.
	Status TicketStatus `json:"status,omitempty"`
	// Priority is a Zoho Desk priority name, or empty for the department's default.
	Priority TicketPriority `json:"priority,omitempty"`
	// AssigneeID assigns the ticket to an agent, or empty for Zoho Desk's routing.
	AssigneeID string `json:"assigneeId,omitempty"`
}

// TicketContactInput identifies a ticket's contact by email for createTicket.
type TicketContactInput struct {
	// Email is one bare address such as jane@example.com.
	Email string `json:"email,omitempty"`
	// FirstName names a contact Zoho Desk adds; blank sends none.
	FirstName string `json:"firstName,omitempty"`
	// LastName names a contact Zoho Desk adds; blank sends none.
	LastName string `json:"lastName,omitempty"`
}

// CreateTicketOutput is the created ticket.
type CreateTicketOutput struct {
	// Ticket is the ticket Zoho Desk created.
	Ticket Ticket `json:"ticket"`
}

// CreateTicketOperation is the createTicket Mutation.
type CreateTicketOperation struct {
	client *Client
}

type createTicketRequestWire struct {
	Subject      string              `json:"subject"`
	Description  string              `json:"description"`
	DepartmentID string              `json:"departmentId"`
	ContactID    string              `json:"contactId,omitempty"`
	Contact      *contactRequestWire `json:"contact,omitempty"`
	Email        string              `json:"email,omitempty"`
	Status       string              `json:"status,omitempty"`
	Priority     string              `json:"priority,omitempty"`
	AssigneeID   string              `json:"assigneeId,omitempty"`
}

type contactRequestWire struct {
	Email     string `json:"email"`
	FirstName string `json:"firstName,omitempty"`
	LastName  string `json:"lastName,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateTicketOperation) Definition() sdkgo.MutationDefinition { return CreateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. Zoho Desk documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (CreateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v1/tickets at most once per Step execution. Only a 429 or a connection
// that never opened is retried; any other unconfirmed outcome selects uncertain without resending.
func (operation CreateTicketOperation) Invoke(call sdkgo.Call, input CreateTicketInput) sdkgo.MutationAttempt[CreateTicketOutput] {
	branches := singleDispatchBranches{providerRejected: CreateTicketBranchProviderRejected, defect: CreateTicketBranchDefect}
	if err := validateCreateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, deskFailurePointer(sdkgo.FailureValidation, createTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	if attempt, isTerminal := singleDispatchAttemptForEarlierDispatch[CreateTicketOutput](call, createTicketOperation,
		operation.client.receipt(call, deskResponse{}, "")); isTerminal {
		return attempt
	}
	session, cancel, failure := operation.client.startSession(call, createTicketOperation)
	if failure != nil {
		return singleDispatchAttemptForSession[CreateTicketOutput](failure, branches)
	}
	defer cancel()
	if attempt, isTerminal := singleDispatchAttemptForRecord[CreateTicketOutput](call, createTicketOperation); isTerminal {
		return attempt
	}
	result := operation.client.exchange(session, createTicketOperation, deskRequest{
		method: http.MethodPost, path: "/tickets", payload: buildCreateTicketRequest(input),
	})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := singleDispatchAttemptForExchange[CreateTicketOutput](call, result, receipt, branches); isTerminal {
		return attempt
	}
	ticket, _, err := decodeTicketBody(result.response.body, "")
	if err != nil {
		return sdkgo.NewMutationUncertain(CreateTicketOutput{}, deskFailure(sdkgo.FailureProtocol, createTicketOperation,
			"Zoho Desk accepted the ticket but returned an invalid ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{Ticket: ticket}, nil, operation.client.receipt(call, result.response, ticket.ID))
}

func validateCreateTicketInput(input CreateTicketInput) error {
	if err := validateTextInput("subject", input.Subject, maximumSubjectRunes); err != nil {
		return err
	}
	if err := validateTextInput("description", input.Description, maximumDescriptionRunes); err != nil {
		return err
	}
	if utf8.RuneCountInString(plainTextToZohoHTML(input.Description)) > maximumDescriptionRunes {
		return errors.New("description is longer than Zoho Desk's 65,535-character limit once escaped as HTML")
	}
	if err := validateZohoID("departmentId", input.DepartmentID); err != nil {
		return err
	}
	hasContactID, hasContactEmail := input.ContactID != "", input.Contact.Email != ""
	switch {
	case hasContactID == hasContactEmail:
		return errors.New("set exactly one of contactId and contact.email")
	case hasContactID:
		if err := validateZohoID("contactId", input.ContactID); err != nil {
			return err
		}
		if input.Contact != (TicketContactInput{}) {
			return errors.New("contact names apply only with contact.email")
		}
	case !isBareEmailAddress(input.Contact.Email):
		return errors.New("contact.email must be one bare email address such as jane@example.com")
	}
	if err := errors.Join(validateContactName("contact.firstName", input.Contact.FirstName), validateContactName("contact.lastName", input.Contact.LastName)); err != nil {
		return err
	}
	if input.Status != "" {
		if err := validatePicklistValue("status", string(input.Status)); err != nil {
			return err
		}
	}
	if input.Priority != "" {
		if err := validatePicklistValue("priority", string(input.Priority)); err != nil {
			return err
		}
	}
	return validateOptionalZohoID("assigneeId", input.AssigneeID)
}

// buildCreateTicketRequest also sets the ticket email to the contact's, which searchTickets' contactEmail filter matches.
func buildCreateTicketRequest(input CreateTicketInput) createTicketRequestWire {
	request := createTicketRequestWire{
		Subject: input.Subject, Description: plainTextToZohoHTML(input.Description), DepartmentID: input.DepartmentID,
		ContactID: input.ContactID, Status: string(input.Status), Priority: string(input.Priority), AssigneeID: input.AssigneeID,
	}
	if input.Contact.Email != "" {
		request.Contact = &contactRequestWire{Email: input.Contact.Email, FirstName: input.Contact.FirstName, LastName: input.Contact.LastName}
		request.Email = input.Contact.Email
	}
	return request
}
