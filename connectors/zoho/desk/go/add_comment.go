// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package desk

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

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
// key only correlates the Receipt; the SDK single-dispatch fence bounds resends instead.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /api/v1/tickets/{id}/comments under the single-dispatch fence and the manifest
// send cap. A 429, 503, or request that never left the process retries; another unknown outcome
// resends while the cap allows and selects uncertain at the cap, because Zoho Desk has no comment read-back by Call ID.
// A 2xx is never resent: a returned or checkpointed comment ID is read back with GET
// /api/v1/tickets/{ticketId}/comments/{commentId} and selects added, and a 2xx without a readable ID selects uncertain.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	branches := singleDispatchBranches{notFound: AddCommentBranchNotFound, providerRejected: AddCommentBranchProviderRejected, defect: AddCommentBranchDefect}
	if err := validateAddCommentInput(input); err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, AddCommentOutput{}, deskFailurePointer(sdkgo.FailureValidation, addCommentOperation, err.Error()), sdkgo.Receipt{})
	}
	permit, priorAttempt := beginDeskWrite[AddCommentOutput](call, operation.client, addCommentOperation,
		AddCommentDefinition.StepDefaults.ExecuteMethodTimeout, AddCommentBranchDefect)
	if priorAttempt != nil {
		return *priorAttempt
	}
	session, cancel, failure := operation.client.startSession(call, addCommentOperation)
	if failure != nil {
		return sessionAttemptForWrite[AddCommentOutput](permit, failure, branches)
	}
	defer cancel()
	readBack := operation.readBackAddedComment(call, session, input)
	if permit.savedProviderID != "" {
		return readBack(permit.savedProviderID, true)
	}
	result, dispatchAttempt := sendDeskWrite[AddCommentOutput](permit, operation.client, session, addCommentOperation, deskRequest{
		method: http.MethodPost, path: ticketPath(input.TicketID) + "/comments",
		payload: addCommentRequestWire{Content: input.Content, IsPublic: input.IsPublic, ContentType: plainTextType},
	})
	if dispatchAttempt != nil {
		return *dispatchAttempt
	}
	receipt := operation.client.receipt(call, result.response, "")
	if attempt, isTerminal := deskWriteAttemptForExchange[AddCommentOutput](permit, call, operation.client, session, result, receipt, branches, readBack); isTerminal {
		return attempt
	}
	comment, err := decodeCommentBody(result.response.body)
	if err != nil {
		return acceptedWriteAttempt(permit, acceptedProviderID(result.response.body), deskFailure(sdkgo.FailureProtocol, addCommentOperation,
			"Zoho Desk accepted the comment but returned an invalid comment: "+err.Error()), receipt, readBack)
	}
	recordAcceptedWrite(permit, comment.ID)
	receipt = operation.client.receipt(call, result.response, comment.ID)
	if comment.IsPublic != input.IsPublic {
		// A read-back would return the same stored visibility.
		return sdkgo.NewMutationUncertain(AddCommentOutput{}, deskFailure(sdkgo.FailureProtocol, addCommentOperation,
			"Zoho Desk accepted the comment but returned an invalid comment: "+errCommentVisibility.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, AddCommentOutput{Comment: comment}, nil, receipt)
}

// readBackAddedComment reads an accepted comment with GET /api/v1/tickets/{ticketId}/comments/{commentId}.
func (operation AddCommentOperation) readBackAddedComment(call sdkgo.Call, session *operationSession, input AddCommentInput) deskWriteReadBack[AddCommentOutput] {
	return func(commentID string, isProviderIDSaved bool) sdkgo.MutationAttempt[AddCommentOutput] {
		return readBackAcceptedDeskWrite(call, operation.client, session, addCommentOperation, commentID, isProviderIDSaved,
			deskRequest{method: http.MethodGet, path: ticketPath(input.TicketID) + "/comments/" + url.PathEscape(commentID)}, AddCommentBranchAdded,
			func(body []byte) (AddCommentOutput, error) {
				comment, err := decodeCommentBody(body)
				switch {
				case err != nil:
					return AddCommentOutput{}, err
				case comment.ID != commentID:
					return AddCommentOutput{}, errors.New("comment response is for another comment")
				case comment.IsPublic != input.IsPublic:
					return AddCommentOutput{}, errCommentVisibility
				}
				return AddCommentOutput{Comment: comment}, nil
			})
	}
}

func validateAddCommentInput(input AddCommentInput) error {
	if err := validateZohoID("ticketId", input.TicketID); err != nil {
		return err
	}
	return validateTextInput("content", input.Content, MaxCommentRunes)
}

var errCommentVisibility = errors.New("comment response has another visibility than requested")

func decodeCommentBody(body []byte) (TicketComment, error) {
	var wire deskCommentWire
	if json.Unmarshal(body, &wire) != nil {
		return TicketComment{}, errors.New("comment response is not a comment object")
	}
	return decodeCommentWire(wire)
}
