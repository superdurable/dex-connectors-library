// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support

import (
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createTicketOperation = "createTicket"

// CreateTicketInput is one new ticket and its first comment. Zero IDs and empty
// enums leave the field to Zendesk's defaults and the account's triggers.
type CreateTicketInput struct {
	// Subject is the ticket subject. It is required.
	Subject string `json:"subject"`
	// Comment is the first comment, which also becomes the ticket description.
	Comment TicketCommentInput `json:"comment"`
	// Requester is the customer asking for help. Nil makes the connection's agent the requester.
	Requester *TicketRequesterInput `json:"requester,omitempty"`
	// Priority is a Zendesk priority, or empty for none.
	Priority TicketPriority `json:"priority,omitempty"`
	// Type is a Zendesk ticket type, or empty for none.
	Type TicketType `json:"type,omitempty"`
	// Tags are lowercase tags set on the new ticket; account triggers may add more.
	Tags []string `json:"tags,omitempty"`
	// GroupID assigns the ticket to a group, or zero for none.
	GroupID int64 `json:"groupId,omitempty"`
	// AssigneeID assigns the ticket to an agent, or zero for none.
	AssigneeID int64 `json:"assigneeId,omitempty"`
	// BrandID places the ticket in a brand, or zero for the account's default brand.
	BrandID int64 `json:"brandId,omitempty"`
	// ExternalID stores an application correlation ID; Zendesk does not enforce uniqueness.
	ExternalID string `json:"externalId,omitempty"`
}

// TicketRequesterInput identifies the requester by email. Zendesk uses an existing
// user with that address or, when the account allows it, creates one named Name.
type TicketRequesterInput struct {
	// Email is one bare address such as jane@example.com.
	Email string `json:"email"`
	// Name is used only when Zendesk creates a new user; blank lets Zendesk derive one.
	Name string `json:"name,omitempty"`
}

// CreateTicketOutput is the created ticket.
type CreateTicketOutput struct {
	// Ticket is the ticket Zendesk created or, for a replayed request, created earlier for this Step.
	Ticket Ticket `json:"ticket"`
	// WasIdempotentReplay reports that Zendesk returned the earlier response cached under the
	// Step's Idempotency-Key instead of creating a ticket.
	WasIdempotentReplay bool `json:"wasIdempotentReplay,omitempty"`
}

// CreateTicketOperation is the createTicket Mutation.
type CreateTicketOperation struct {
	client *Client
}

type createTicketRequestWire struct {
	Ticket createTicketWire `json:"ticket"`
}

type createTicketWire struct {
	Subject    string            `json:"subject"`
	Comment    ticketCommentWire `json:"comment"`
	Requester  *requesterWire    `json:"requester,omitempty"`
	Priority   string            `json:"priority,omitempty"`
	Type       string            `json:"type,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	GroupID    int64             `json:"group_id,omitempty"`
	AssigneeID int64             `json:"assignee_id,omitempty"`
	BrandID    int64             `json:"brand_id,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
	Metadata   map[string]string `json:"metadata"`
}

type ticketCommentWire struct {
	Body   string `json:"body"`
	Public bool   `json:"public"`
}

type requesterWire struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateTicketOperation) Definition() sdkgo.MutationDefinition { return CreateTicketDefinition }

// IdempotencyKey uses the stable connector Call ID, so every attempt of one Step execution
// sends the same Zendesk Idempotency-Key. Zendesk keeps a key for two hours.
func (CreateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates the ticket under the call's Idempotency-Key. A lost response, a 409 from a
// concurrent attempt with the same key, a rate limit, or a 5xx retries the identical request,
// and Zendesk answers a repeated key and body with the ticket it already created.
func (operation CreateTicketOperation) Invoke(call sdkgo.Call, input CreateTicketInput) sdkgo.MutationAttempt[CreateTicketOutput] {
	if err := validateCreateTicketInput(input); err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureValidation, createTicketOperation, err.Error()), sdkgo.Receipt{})
	}
	if call.IdempotencyKey == "" {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureLocalDefect, createTicketOperation, "connector call has no idempotency key"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, createTicketOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, failure, sdkgo.Receipt{})
	}
	result := operation.client.exchange(call, credentials, createTicketOperation, zendeskRequest{
		method: http.MethodPost, path: "/tickets", payload: buildCreateTicketRequest(input, call.IdempotencyKey),
		idempotencyKey: call.IdempotencyKey,
	})
	receipt := operation.client.receipt(call, result.response, 0)
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeRetryable:
		return sdkgo.NewMutationRetry[CreateTicketOutput](result.failure, result.retryAfter)
	case exchangeInvalid:
		return sdkgo.NewMutationBranch(CreateTicketBranchInvalidResponse, CreateTicketOutput{}, &result.failure, receipt)
	case exchangeDefect:
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, CreateTicketOutput{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateTicketBranchProviderRejected, CreateTicketOutput{}, &result.failure, receipt)
	}
	decoded, err := decodeTicketEnvelope(result.response.body, operation.client.agentTicketURL)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchInvalidResponse, CreateTicketOutput{}, zendeskFailurePointer(sdkgo.FailureProtocol, createTicketOperation, "Zendesk returned an invalid created ticket: "+err.Error()), receipt)
	}
	receipt = operation.client.receipt(call, result.response, decoded.ticket.ID)
	lookup := strings.ToLower(result.response.header.Get(idempotencyLookupHeader))
	if lookup != "" {
		receipt.Metadata = map[string]string{"idempotencyLookup": lookup}
	}
	return sdkgo.NewMutationBranch(CreateTicketBranchCreated, CreateTicketOutput{
		Ticket: decoded.ticket, WasIdempotentReplay: lookup == "hit",
	}, nil, receipt)
}

func validateCreateTicketInput(input CreateTicketInput) error {
	if strings.TrimSpace(input.Subject) == "" {
		return errors.New("subject is required")
	}
	if err := validateCommentInput("comment", input.Comment); err != nil {
		return err
	}
	if input.Requester != nil && !isBareEmailAddress(input.Requester.Email) {
		return errors.New("requester.email must be one bare email address such as jane@example.com")
	}
	if input.Priority != "" {
		if err := validateTicketPriority(input.Priority); err != nil {
			return err
		}
	}
	if input.Type != "" {
		if err := validateTicketType(input.Type); err != nil {
			return err
		}
	}
	if err := validateTags("tags", input.Tags); err != nil {
		return err
	}
	return errors.Join(
		validateOptionalID("groupId", input.GroupID), validateOptionalID("assigneeId", input.AssigneeID),
		validateOptionalID("brandId", input.BrandID),
	)
}

// buildCreateTicketRequest is deterministic for one input, because Zendesk rejects a
// repeated Idempotency-Key whose body differs from the first request.
func buildCreateTicketRequest(input CreateTicketInput, idempotencyKey sdkgo.IdempotencyKey) createTicketRequestWire {
	ticket := createTicketWire{
		Subject: input.Subject, Comment: ticketCommentWire{Body: input.Comment.Body, Public: !input.Comment.IsInternalNote},
		Priority: string(input.Priority), Type: string(input.Type), Tags: input.Tags,
		GroupID: input.GroupID, AssigneeID: input.AssigneeID, BrandID: input.BrandID, ExternalID: input.ExternalID,
		Metadata: map[string]string{idempotencyMetadataKey: string(idempotencyKey)},
	}
	if input.Requester != nil {
		ticket.Requester = &requesterWire{Email: input.Requester.Email, Name: input.Requester.Name}
	}
	return createTicketRequestWire{Ticket: ticket}
}
