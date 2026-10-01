// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createTicketOperation = "createTicket"

// CreateTicketInput is one new ticket. Zero values leave a field to Freshdesk's defaults, the
// account's ticket form, and its automations.
type CreateTicketInput struct {
	// Subject is the ticket subject. It is required.
	Subject string `json:"subject"`
	// Description is the ticket's plain-text content. It is required. The connector escapes it
	// into Freshdesk's HTML description, so markup is shown literally and line breaks are kept.
	Description string `json:"description"`
	// Requester is the customer asking for help. It is required.
	Requester TicketRequesterInput `json:"requester"`
	// Status is a Freshdesk status, or zero for Freshdesk's default, Open (2).
	Status TicketStatus `json:"status,omitempty"`
	// Priority is a Freshdesk priority, or zero for Freshdesk's default, Low (1).
	Priority TicketPriority `json:"priority,omitempty"`
	// Type is one of the account's ticket type labels, such as Question, or empty for none.
	Type string `json:"type,omitempty"`
	// Tags are set on the new ticket; account automations may add more.
	Tags []string `json:"tags,omitempty"`
	// GroupID assigns the ticket to a group, or zero for Freshdesk's default routing.
	GroupID int64 `json:"groupId,omitempty"`
	// ResponderID assigns the ticket to an agent, Freshdesk's responder_id, or zero for none.
	ResponderID int64 `json:"responderId,omitempty"`
}

// TicketRequesterInput identifies the requester by email. Freshdesk uses the contact with that
// address or, when none exists, adds a contact named Name.
type TicketRequesterInput struct {
	// Email is one bare address such as jane@example.com.
	Email string `json:"email"`
	// Name names a contact Freshdesk adds; blank lets Freshdesk derive one.
	Name string `json:"name,omitempty"`
}

// CreateTicketOutput is the created ticket.
type CreateTicketOutput struct {
	// Ticket is the ticket Freshdesk created.
	Ticket Ticket `json:"ticket"`
}

// CreateTicketOperation is the createTicket Mutation.
type CreateTicketOperation struct {
	client *Client
}

type createTicketRequestWire struct {
	Subject     string   `json:"subject"`
	Description string   `json:"description"`
	Email       string   `json:"email"`
	Name        string   `json:"name,omitempty"`
	Status      int      `json:"status,omitempty"`
	Priority    int      `json:"priority,omitempty"`
	Type        string   `json:"type,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	GroupID     int64    `json:"group_id,omitempty"`
	ResponderID int64    `json:"responder_id,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateTicketOperation) Definition() sdkgo.MutationDefinition { return CreateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID. Freshdesk documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (CreateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v2/tickets at most once per Step execution. Only a 429 or a connection
// that never opened is retried; any other unconfirmed outcome selects uncertain without resending.
func (operation CreateTicketOperation) Invoke(call sdkgo.Call, input CreateTicketInput) sdkgo.MutationAttempt[CreateTicketOutput] {
	if err := validateCreateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, freshdeskFailurePointer(sdkgo.FailureValidation, createTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, createTicketOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	if attempt, isTerminal := singleDispatchAttemptBeforeSend[CreateTicketOutput](claimSingleDispatch(call), createTicketOperation,
		operation.client.receipt(call, freshdeskResponse{}, 0)); isTerminal {
		return attempt
	}
	result := operation.client.exchange(call, credentials, createTicketOperation, freshdeskRequest{
		method: http.MethodPost, path: "/tickets", payload: buildCreateTicketRequest(input),
	})
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := singleDispatchAttemptForExchange[CreateTicketOutput](call, result, receipt, singleDispatchBranches{
		providerRejected: CreateTicketBranchProviderRejected, defect: CreateTicketBranchDefect,
	}); isTerminal {
		return attempt
	}
	ticket, err := decodeTicketBody(result.response.body, 0)
	if err != nil {
		return sdkgo.NewMutationUncertain(CreateTicketOutput{}, freshdeskFailure(sdkgo.FailureProtocol, createTicketOperation,
			"Freshdesk accepted the ticket but returned an invalid ticket: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{Ticket: ticket}, nil, operation.client.receipt(call, result.response, ticket.ID))
}

func validateCreateTicketInput(input CreateTicketInput) error {
	if err := validateTextInput("subject", input.Subject); err != nil {
		return err
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
	if input.Status != 0 {
		if err := validateTicketStatus("status", input.Status); err != nil {
			return err
		}
	}
	if input.Priority != 0 {
		if err := validateTicketPriority("priority", input.Priority); err != nil {
			return err
		}
	}
	if input.Type != "" {
		if err := validateTicketType(input.Type); err != nil {
			return err
		}
	}
	return errors.Join(validateTags("tags", input.Tags), validateOptionalID("groupId", input.GroupID), validateOptionalID("responderId", input.ResponderID))
}

func buildCreateTicketRequest(input CreateTicketInput) createTicketRequestWire {
	return createTicketRequestWire{
		Subject: input.Subject, Description: plainTextToFreshdeskHTML(input.Description),
		Email: input.Requester.Email, Name: input.Requester.Name,
		Status: int(input.Status), Priority: int(input.Priority), Type: input.Type, Tags: input.Tags,
		GroupID: input.GroupID, ResponderID: input.ResponderID,
	}
}
