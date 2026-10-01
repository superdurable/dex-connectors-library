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
	createCardOperationID    = "createCard"
	createCardFailureSubject = "card"
)

// CreateCardInput describes one card in one list.
type CreateCardInput struct {
	// ListID is the ID of the list that receives the card; the list also decides the card's board.
	ListID string `json:"listId"`
	// Name is the one-line card name, at most 16384 characters.
	Name string `json:"name"`
	// Description is the plain-text description, at most 16384 characters; blank sends none.
	Description string `json:"description,omitempty"`
	// Due is when the card is due; nil sends none.
	Due *time.Time `json:"due,omitempty"`
	// LabelIDs adds up to 50 labels of the list's board by ID.
	LabelIDs []string `json:"labelIds,omitempty"`
	// MemberIDs adds up to 50 board members to the card by ID.
	MemberIDs []string `json:"memberIds,omitempty"`
	// Position is top, bottom, or a positive number; blank keeps Trello's default position.
	Position string `json:"position,omitempty"`
}

// CreateCardOutput identifies the created card. On providerRejected and uncertain, CardID is empty and the
// requested name and list are echoed so the application can reconcile.
type CreateCardOutput struct {
	// CardID is the new card's Trello ID.
	CardID string `json:"cardId,omitempty"`
	// Name echoes the requested name.
	Name string `json:"name"`
	// ListID echoes the requested list.
	ListID string `json:"listId"`
	// BoardID is the board that holds the new card.
	BoardID string `json:"boardId,omitempty"`
	// ShortLink is the 8-character code in the new card's web address.
	ShortLink string `json:"shortLink,omitempty"`
	// URL is the new card's full Trello web URL.
	URL string `json:"url,omitempty"`
	// ShortURL is the new card's short Trello web URL.
	ShortURL string `json:"shortUrl,omitempty"`
}

// CreateCardOperation implements the createCard Mutation.
type CreateCardOperation struct{ client *Client }

// createCardFields is Trello's JSON body for POST /cards; ID lists are comma-separated as documented.
type createCardFields struct {
	IDList    string `json:"idList"`
	Name      string `json:"name"`
	Desc      string `json:"desc,omitempty"`
	Due       string `json:"due,omitempty"`
	IDLabels  string `json:"idLabels,omitempty"`
	IDMembers string `json:"idMembers,omitempty"`
	Pos       any    `json:"pos,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (CreateCardOperation) Definition() sdkgo.MutationDefinition { return CreateCardDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Trello accepts no
// idempotency key, so it is never sent; single dispatch comes from a Dex heartbeat checkpoint instead.
func (CreateCardOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCardInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /cards at most once per Step execution. Only a 429 or a connection that never opened is
// retried; any other unconfirmed outcome selects uncertain without resending.
func (operation CreateCardOperation) Invoke(call sdkgo.Call, input CreateCardInput) sdkgo.MutationAttempt[CreateCardOutput] {
	client := operation.client
	fields, requested, err := buildCreateCardFields(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCardBranchDefect, requested, failurePointer(createCardOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, createCardOperationID)
	if startFailure != nil {
		return sdkgo.NewMutationBranch(CreateCardBranchDefect, requested, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	if attempt, isFinal := singleDispatchAttemptBeforeSend(claimSingleDispatch(call), createCardOperationID, requested,
		client.receipt(session, trelloResponse{}, "")); isFinal {
		return attempt
	}
	result := client.exchange(session, trelloRequest{method: http.MethodPost, path: "/cards", payload: fields})
	receipt := client.receipt(session, result.response, "")
	if attempt, isFinal := singleDispatchAttemptForWrite(call, client.classifyUnkeyedWrite(createCardOperationID, createCardFailureSubject, result),
		requested, receipt, singleDispatchBranches{providerRejected: CreateCardBranchProviderRejected, defect: CreateCardBranchDefect}); isFinal {
		return attempt
	}
	created, err := decodeJSONObject[cardResource](result.response.body)
	if err != nil || !trelloIDPattern.MatchString(created.ID) {
		return sdkgo.NewMutationUncertain(requested, newFailure(createCardOperationID, sdkgo.FailureProtocol, "Trello accepted the card but returned an unusable card reference"), receipt)
	}
	output := requested
	output.CardID, output.URL, output.ShortURL = created.ID, created.URL, created.ShortURL
	if trelloIDPattern.MatchString(created.IDBoard) {
		output.BoardID = created.IDBoard
	}
	if shortLinkPattern.MatchString(created.ShortLink) {
		output.ShortLink = created.ShortLink
	}
	receipt.ProviderObjectID = created.ID
	return sdkgo.NewMutationBranch(CreateCardBranchCreated, output, nil, receipt)
}

// buildCreateCardFields validates input and returns the request plus the echo used by every branch.
func buildCreateCardFields(input CreateCardInput) (createCardFields, CreateCardOutput, error) {
	requested := CreateCardOutput{Name: strings.TrimSpace(input.Name), ListID: strings.TrimSpace(input.ListID)}
	listID, err := validateTrelloID(input.ListID, "listId")
	if err != nil {
		return createCardFields{}, requested, err
	}
	name, err := validateCardName(input.Name)
	if err != nil {
		return createCardFields{}, requested, err
	}
	fields := createCardFields{IDList: listID, Name: name}
	if err := validatePlainText(input.Description, "description", false); err != nil {
		return createCardFields{}, requested, err
	}
	if strings.TrimSpace(input.Description) != "" {
		fields.Desc = input.Description
	}
	if input.Due != nil {
		fields.Due = formatTrelloTime(*input.Due)
	}
	labelIDs, err := validateTrelloIDs(input.LabelIDs, "labelIds")
	if err != nil {
		return createCardFields{}, requested, err
	}
	memberIDs, err := validateTrelloIDs(input.MemberIDs, "memberIds")
	if err != nil {
		return createCardFields{}, requested, err
	}
	fields.IDLabels, fields.IDMembers = strings.Join(labelIDs, ","), strings.Join(memberIDs, ",")
	if fields.Pos, err = cardPositionWireValue(input.Position); err != nil {
		return createCardFields{}, requested, err
	}
	return fields, requested, nil
}
