// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	addCommentOperationID    = "addComment"
	addCommentFailureSubject = "comment"
)

// AddCommentInput describes one comment on one card.
type AddCommentInput struct {
	// CardID is the card's Trello ID.
	CardID string `json:"cardId"`
	// Text is the plain-text comment, at most 16384 characters; line breaks are kept.
	Text string `json:"text"`
}

// AddCommentOutput identifies the added comment. On notFound, providerRejected, and uncertain, CommentID is
// empty and CardID echoes the request.
type AddCommentOutput struct {
	// CardID echoes the requested card.
	CardID string `json:"cardId"`
	// CommentID is the ID of Trello's commentCard action that holds the comment.
	CommentID string `json:"commentId,omitempty"`
	// AuthorMemberID is the member that wrote the comment, the token's member.
	AuthorMemberID string `json:"authorMemberId,omitempty"`
	// CreatedAt is when Trello stored the comment.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
}

// AddCommentOperation implements the addComment Mutation.
type AddCommentOperation struct{ client *Client }

type addCommentFields struct {
	Text string `json:"text"`
}

type commentActionResource struct {
	ID              string `json:"id"`
	IDMemberCreator string `json:"idMemberCreator"`
	Date            string `json:"date"`
}

// Definition returns the immutable connector operation definition.
func (AddCommentOperation) Definition() sdkgo.MutationDefinition { return AddCommentDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Trello accepts no
// idempotency key, so it is never sent; single dispatch comes from a Dex heartbeat checkpoint instead.
func (AddCommentOperation) IdempotencyKey(callID sdkgo.CallID, _ AddCommentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /cards/{id}/actions/comments at most once per Step execution. Only a 429 or a
// connection that never opened is retried; any other unconfirmed outcome selects uncertain without resending.
func (operation AddCommentOperation) Invoke(call sdkgo.Call, input AddCommentInput) sdkgo.MutationAttempt[AddCommentOutput] {
	client := operation.client
	requested := AddCommentOutput{CardID: strings.TrimSpace(input.CardID)}
	cardID, err := validateTrelloID(input.CardID, "cardId")
	if err == nil {
		err = validatePlainText(input.Text, "text", true)
	}
	if err != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, failurePointer(addCommentOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, addCommentOperationID)
	if startFailure != nil {
		return sdkgo.NewMutationBranch(AddCommentBranchDefect, requested, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	if attempt, isFinal := singleDispatchAttemptBeforeSend(claimSingleDispatch(call), addCommentOperationID, requested,
		client.receipt(session, trelloResponse{}, cardID)); isFinal {
		return attempt
	}
	result := client.exchange(session, trelloRequest{
		method: http.MethodPost, path: joinPath("cards", cardID, "actions", "comments"), payload: addCommentFields{Text: input.Text},
	})
	receipt := client.receipt(session, result.response, cardID)
	if attempt, isFinal := singleDispatchAttemptForWrite(call, client.classifyUnkeyedWrite(addCommentOperationID, addCommentFailureSubject, result),
		requested, receipt, singleDispatchBranches{
			notFound: AddCommentBranchNotFound, providerRejected: AddCommentBranchProviderRejected, defect: AddCommentBranchDefect,
		}); isFinal {
		return attempt
	}
	action, err := decodeJSONObject[commentActionResource](result.response.body)
	if err != nil || !trelloIDPattern.MatchString(action.ID) {
		return sdkgo.NewMutationUncertain(requested, newFailure(addCommentOperationID, sdkgo.FailureProtocol, "Trello accepted the comment but returned an unusable action reference"), receipt)
	}
	output := requested
	output.CommentID = action.ID
	if trelloIDPattern.MatchString(action.IDMemberCreator) {
		output.AuthorMemberID = action.IDMemberCreator
	}
	// The comment exists either way; an unparseable timestamp only leaves CreatedAt nil.
	if createdAt, err := parseOptionalTrelloTime(&action.Date); err == nil {
		output.CreatedAt = createdAt
	}
	receipt.ProviderObjectID = action.ID
	return sdkgo.NewMutationBranch(AddCommentBranchAdded, output, nil, receipt)
}
