// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshdesk

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const addNoteOperation = "addNote"

// AddNoteInput is one private note or one public reply to add to a ticket.
type AddNoteInput struct {
	// TicketID is the Freshdesk ticket ID.
	TicketID int64 `json:"ticketId"`
	// Body is the plain-text note or reply. It is required. The connector escapes it into
	// Freshdesk's HTML body, so markup is shown literally and line breaks are kept.
	Body string `json:"body"`
	// IsPublicReply sends the body as a reply that Freshdesk emails to the requester, through
	// POST /api/v2/tickets/{id}/reply. False adds a private note that only agents can see.
	IsPublicReply bool `json:"isPublicReply,omitempty"`
}

// AddNoteOutput is the added note or reply.
type AddNoteOutput struct {
	// Conversation is the note or reply Freshdesk added.
	Conversation TicketConversation `json:"conversation"`
}

// AddNoteOperation is the addNote Mutation.
type AddNoteOperation struct {
	client *Client
}

type privateNoteRequestWire struct {
	Body    string `json:"body"`
	Private bool   `json:"private"`
}

type publicReplyRequestWire struct {
	Body string `json:"body"`
}

// Definition returns the immutable connector operation definition.
func (AddNoteOperation) Definition() sdkgo.MutationDefinition { return AddNoteDefinition }

// IdempotencyKey uses the stable connector Call ID. Freshdesk documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (AddNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ AddNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v2/tickets/{id}/notes or /reply at most once per Step execution. Only a
// 429 or a connection that never opened is retried; any other unconfirmed outcome selects
// uncertain without resending, because a repeated reply is a second email to the requester.
func (operation AddNoteOperation) Invoke(call sdkgo.Call, input AddNoteInput) sdkgo.MutationAttempt[AddNoteOutput] {
	if err := validateAddNoteInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, freshdeskFailurePointer(sdkgo.FailureValidation, addNoteOperation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, addNoteOperation)
	if failure != nil {
		return sdkgo.NewMutationBranch(AddNoteBranchDefect, AddNoteOutput{}, failure, sdkgo.Receipt{})
	}
	if attempt, isTerminal := singleDispatchAttemptBeforeSend[AddNoteOutput](claimSingleDispatch(call), addNoteOperation,
		operation.client.receipt(call, freshdeskResponse{}, 0)); isTerminal {
		return attempt
	}
	request := freshdeskRequest{method: http.MethodPost, path: ticketPath(input.TicketID) + "/notes",
		payload: privateNoteRequestWire{Body: plainTextToFreshdeskHTML(input.Body), Private: true}}
	if input.IsPublicReply {
		request = freshdeskRequest{method: http.MethodPost, path: ticketPath(input.TicketID) + "/reply",
			payload: publicReplyRequestWire{Body: plainTextToFreshdeskHTML(input.Body)}}
	}
	result := operation.client.exchange(call, credentials, addNoteOperation, request)
	receipt := operation.client.receipt(call, result.response, 0)
	if attempt, isTerminal := singleDispatchAttemptForExchange[AddNoteOutput](call, result, receipt, singleDispatchBranches{
		notFound: AddNoteBranchNotFound, providerRejected: AddNoteBranchProviderRejected, defect: AddNoteBranchDefect,
	}); isTerminal {
		return attempt
	}
	conversation, err := decodeAddedConversation(result.response.body, input)
	if err != nil {
		return sdkgo.NewMutationUncertain(AddNoteOutput{}, freshdeskFailure(sdkgo.FailureProtocol, addNoteOperation,
			"Freshdesk accepted the note but returned an invalid conversation: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(AddNoteBranchAdded, AddNoteOutput{Conversation: conversation}, nil, operation.client.receipt(call, result.response, conversation.ID))
}

func validateAddNoteInput(input AddNoteInput) error {
	if input.TicketID < 1 {
		return errors.New("ticketId must be a positive Freshdesk ticket ID")
	}
	return validateTextInput("body", input.Body)
}

// decodeAddedConversation fills the kind from the request, because a reply response has no private or source field.
func decodeAddedConversation(body []byte, input AddNoteInput) (TicketConversation, error) {
	var wire freshdeskConversationWire
	var visibility struct {
		Private *bool `json:"private"`
	}
	if json.Unmarshal(body, &wire) != nil || json.Unmarshal(body, &visibility) != nil {
		return TicketConversation{}, errors.New("conversation response is not a conversation object")
	}
	conversation, err := decodeConversationWire(wire)
	if err != nil {
		return TicketConversation{}, err
	}
	if conversation.TicketID != 0 && conversation.TicketID != input.TicketID {
		return TicketConversation{}, errors.New("conversation response is for another ticket")
	}
	conversation.TicketID = input.TicketID
	if input.IsPublicReply {
		conversation.Source, conversation.IsPrivate = ConversationSourceReply, false
		return conversation, nil
	}
	conversation.Source, conversation.IsPrivate = ConversationSourceNote, visibility.Private == nil || *visibility.Private
	return conversation, nil
}
