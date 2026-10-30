// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getCardOperationID    = "getCard"
	getCardFailureSubject = "card"
)

// cardMemberFields are the member fields getCard requests; email addresses are never read.
var cardMemberFields = []string{"fullName", "username"}

// GetCardInput identifies one card.
type GetCardInput struct {
	// CardID is the card's Trello ID, 24 hexadecimal characters.
	CardID string `json:"cardId"`
}

// GetCardOperation implements the getCard Query.
type GetCardOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetCardOperation) Definition() sdkgo.QueryDefinition { return GetCardDefinition }

// Invoke reads one card. Transport failures, 408, 429, and 5xx responses are retried.
func (operation GetCardOperation) Invoke(call sdkgo.Call, input GetCardInput) sdkgo.QueryAttempt[Card] {
	client := operation.client
	cardID, err := validateTrelloID(input.CardID, "cardId")
	if err != nil {
		return sdkgo.NewQueryBranch(GetCardBranchDefect, Card{}, failurePointer(getCardOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, getCardOperationID)
	if startFailure != nil {
		return sdkgo.NewQueryBranch(GetCardBranchDefect, Card{}, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	card, receipt, classification := client.readCard(session, getCardOperationID, cardID)
	switch classification.outcome {
	case readSucceeded:
		return sdkgo.NewQueryBranch(GetCardBranchFound, card, nil, receipt)
	case readRetry:
		return sdkgo.NewQueryRetry[Card](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(GetCardBranchNotFound, Card{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(GetCardBranchDefect, Card{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(GetCardBranchInvalidResponse, Card{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(GetCardBranchProviderRejected, Card{}, &classification.failure, receipt)
	}
}

// readCard reads one card with its description, members, and list; getCard and updateCard share it.
func (client *Client) readCard(session *operationSession, operationID string, cardID string) (Card, sdkgo.Receipt, readClassification) {
	result := client.exchange(session, trelloRequest{
		method: http.MethodGet, path: joinPath("cards", cardID), query: url.Values{
			"fields": {strings.Join(cardDetailFields, ",")}, "members": {"true"},
			"member_fields": {strings.Join(cardMemberFields, ",")}, "list": {"true"},
		},
	})
	receipt := client.receipt(session, result.response, cardID)
	classification := client.classifyRead(operationID, getCardFailureSubject, result)
	if classification.outcome != readSucceeded {
		return Card{}, receipt, classification
	}
	resource, err := decodeJSONObject[cardResource](result.response.body)
	var card Card
	if err == nil {
		card, err = decodeCard(resource, true)
	}
	if err == nil && !strings.EqualFold(card.ID, cardID) {
		err = errMismatchedCard
	}
	if err != nil {
		return Card{}, receipt, readClassification{outcome: readInvalid, failure: newFailure(operationID, sdkgo.FailureProtocol, "Trello returned an invalid card: "+err.Error())}
	}
	return card, receipt, classification
}
