// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// MaxCommentRunes is Zoho Desk's limit for a comment's content.
	MaxCommentRunes = 32000

	addCommentOperation = "addComment"
	plainTextType       = "plainText"
)

// AddCommentInput is one plain-text comment to add to a ticket.
type AddCommentInput struct {
	// TicketID is the Zoho Desk ticket ID.
	TicketID string `json:"ticketId"`
	// Content is the plain-text comment, at most MaxCommentRunes characters. It is required. The
	// connector sends it as plainText, so markup is shown literally.
	Content string `json:"content"`
	// IsPublic adds a public comment, which the customer can see in the help center. False adds a
	// private comment that only agents see. Zoho Desk fixes visibility when the comment is added.
	IsPublic bool `json:"isPublic,omitempty"`
}

// AddCommentOutput is the added comment.
type AddCommentOutput struct {
	// Comment is the comment Zoho Desk added.
	Comment TicketComment `json:"comment"`
}

// AddCommentOperation is the addComment Mutation.
type AddCommentOperation struct {
	client *Client
}

type addCommentRequestWire struct {
	Content     string `json:"content"`
	IsPublic    bool   `json:"isPublic"`
	ContentType string `json:"contentType"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey uses the stable connector Call ID. Zoho Desk documents no idempotency key, so the
// key only correlates the Receipt; single dispatch comes from a Dex heartbeat checkpoint instead.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v1/tickets/{id}/comments at most once per Step execution. Only a 429 or a
// connection that never opened is retried; any other unconfirmed outcome selects uncertain without
// resending, because a repeated public comment is shown to the customer twice.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	branches := singleDispatchBranches{notFound: AddCommentBranchNotFound, providerRejected: AddCommentBranchProviderRejected, defect: AddCommentBranchDefect}
	if err := validateAddCommentInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, AddCommentOutput{}, deskFailurePointer(sdkgo.FailureValidation, addCommentOperation, err.Error()), sdkgo.Receipt{})
	}
	if attempt, isTerminal := singleDispatchAttemptForEarlierDispatch[AddCommentOutput](call, addCommentOperation,
		operation.client.receipt(call, deskResponse{}, "")); isTerminal {
		return attempt
	}
	session, cancel, failure := operation.client.startSession(call, addCommentOperation)
	if failure != nil {
		return singleDispatchAttemptForSession[AddCommentOutput](failure, branches)
	}
	defer cancel()
	if attempt, isTerminal := singleDispatchAttemptForRecord[AddCommentOutput](call, addCommentOperation); isTerminal {
		return attempt
	}
	result := operation.client.exchange(session, addCommentOperation, deskRequest{
		method: http.MethodPost, path: ticketPath(input.TicketID) + "/comments",
		payload: addCommentRequestWire{Content: input.Content, IsPublic: input.IsPublic, ContentType: plainTextType},
	})
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := singleDispatchAttemptForExchange[AddCommentOutput](call, result, receipt, branches); isTerminal {
		return attempt
	}
	comment, err := decodeAddedComment(result.response.body, input)
	if err != nil {
		return sdkgo.NewMutationUncertain(AddCommentOutput{}, deskFailure(sdkgo.FailureProtocol, addCommentOperation,
			"Zoho Desk accepted the comment but returned an invalid comment: "+err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, AddCommentOutput{Comment: comment}, nil, operation.client.receipt(call, result.response, comment.ID))
}

func validateAddCommentInput(input AddCommentInput) error {
	if err := validateZohoID("ticketId", input.TicketID); err != nil {
		return err
	}
	return validateTextInput("content", input.Content, MaxCommentRunes)
}

// decodeAddedComment requires the visibility Zoho Desk stored to be the one requested.
func decodeAddedComment(body []byte, input AddCommentInput) (TicketComment, error) {
	var wire deskCommentWire
	if json.Unmarshal(body, &wire) != nil {
		return TicketComment{}, errors.New("comment response is not a comment object")
	}
	comment, err := decodeCommentWire(wire)
	if err != nil {
		return TicketComment{}, err
	}
	if comment.IsPublic != input.IsPublic {
		return TicketComment{}, errors.New("comment response has another visibility than requested")
	}
	return comment, nil
}
