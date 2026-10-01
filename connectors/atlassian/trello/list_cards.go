// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	listCardsOperationID    = "listCards"
	listCardsFailureSubject = "card list"
	defaultCardPageSize     = 50
	maximumCardPageSize     = 100
	// newestFirstSort is the sort Trello documents for nested card lists, newest card ID first.
	newestFirstSort = "-id"
)

// CardStatusFilter selects cards by their archive state.
type CardStatusFilter string

const (
	// CardStatusOpen lists cards that are not archived; it is the default.
	CardStatusOpen CardStatusFilter = "open"
	// CardStatusClosed lists archived cards only.
	CardStatusClosed CardStatusFilter = "closed"
	// CardStatusAll lists open and archived cards.
	CardStatusAll CardStatusFilter = "all"
)

// ListCardsInput selects one page of cards. Set exactly one of BoardID and ListID. Trello filters by Status;
// the connector applies the due filters to each page, so a page can hold fewer matching cards than
// PageSize, even none, while NextBefore is set.
type ListCardsInput struct {
	// BoardID lists the cards of this board ID.
	BoardID string `json:"boardId,omitempty"`
	// ListID lists the cards of this list ID.
	ListID string `json:"listId,omitempty"`
	// Status selects open, closed (archived), or all cards; blank lists open cards.
	Status CardStatusFilter `json:"status,omitempty"`
	// DueBefore keeps only cards due strictly before this instant.
	DueBefore *time.Time `json:"dueBefore,omitempty"`
	// DueAfter keeps only cards due at or after this instant.
	DueAfter *time.Time `json:"dueAfter,omitempty"`
	// HasDueDate keeps only cards with a due date when true and only cards without one when false.
	HasDueDate *bool `json:"hasDueDate,omitempty"`
	// IsDueComplete keeps only cards whose due date is marked complete when true, or not complete when false.
	IsDueComplete *bool `json:"isDueComplete,omitempty"`
	// PageSize is the number of cards to read from Trello, 1 to 100; zero requests 50.
	PageSize int `json:"pageSize,omitempty"`
	// Before continues a previous list with the same filters from its NextBefore; blank reads the newest
	// cards first.
	Before string `json:"before,omitempty"`
}

// ListCardsOutput is one page of cards, newest first.
type ListCardsOutput struct {
	// Cards lists the page's cards that match the due filters, without descriptions.
	Cards []Card `json:"cards"`
	// ScannedCardCount is the number of cards Trello returned for the page before the due filters.
	ScannedCardCount int `json:"scannedCardCount"`
	// NextBefore continues the list with older cards; it is empty when Trello returned a short page.
	NextBefore string `json:"nextBefore,omitempty"`
}

// ListCardsOperation implements the listCards Query with GET /boards/{id}/cards or GET /lists/{id}/cards.
type ListCardsOperation struct{ client *Client }

type cardPageRequest struct {
	path    string
	query   url.Values
	filters cardDueFilters
	size    int
}

type cardDueFilters struct {
	dueBefore     *time.Time
	dueAfter      *time.Time
	hasDueDate    *bool
	isDueComplete *bool
}

// Definition returns the immutable connector operation definition.
func (ListCardsOperation) Definition() sdkgo.QueryDefinition { return ListCardsDefinition }

// Invoke reads one page. Transport failures, 408, 429, and 5xx responses are retried.
func (operation ListCardsOperation) Invoke(call sdkgo.Call, input ListCardsInput) sdkgo.QueryAttempt[ListCardsOutput] {
	client := operation.client
	request, err := buildCardPageRequest(input)
	if err != nil {
		return sdkgo.NewQueryBranch(ListCardsBranchDefect, ListCardsOutput{}, failurePointer(listCardsOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, listCardsOperationID)
	if startFailure != nil {
		return sdkgo.NewQueryBranch(ListCardsBranchDefect, ListCardsOutput{}, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	result := client.exchange(session, trelloRequest{method: http.MethodGet, path: request.path, query: request.query})
	classification := client.classifyRead(listCardsOperationID, listCardsFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	switch classification.outcome {
	case readSucceeded:
	case readRetry:
		return sdkgo.NewQueryRetry[ListCardsOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewQueryBranch(ListCardsBranchNotFound, ListCardsOutput{}, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewQueryBranch(ListCardsBranchDefect, ListCardsOutput{}, &classification.failure, receipt)
	case readInvalid:
		return sdkgo.NewQueryBranch(ListCardsBranchInvalidResponse, ListCardsOutput{}, &classification.failure, receipt)
	default:
		return sdkgo.NewQueryBranch(ListCardsBranchProviderRejected, ListCardsOutput{}, &classification.failure, receipt)
	}
	output, err := decodeCardPage(result.response.body, request)
	if err != nil {
		return sdkgo.NewQueryBranch(ListCardsBranchInvalidResponse, ListCardsOutput{}, failurePointer(listCardsOperationID, sdkgo.FailureProtocol, "Trello returned an invalid card page: "+err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(ListCardsBranchListed, output, nil, receipt)
}

func buildCardPageRequest(input ListCardsInput) (cardPageRequest, error) {
	boardID, err := validateOptionalTrelloID(input.BoardID, "boardId")
	if err != nil {
		return cardPageRequest{}, err
	}
	listID, err := validateOptionalTrelloID(input.ListID, "listId")
	if err != nil {
		return cardPageRequest{}, err
	}
	request := cardPageRequest{filters: cardDueFilters{
		dueBefore: input.DueBefore, dueAfter: input.DueAfter, hasDueDate: input.HasDueDate, isDueComplete: input.IsDueComplete,
	}}
	switch {
	case (boardID == "") == (listID == ""):
		return cardPageRequest{}, errors.New("set exactly one of boardId and listId")
	case boardID != "":
		request.path = joinPath("boards", boardID, "cards")
	default:
		request.path = joinPath("lists", listID, "cards")
	}
	status := input.Status
	switch status {
	case "":
		status = CardStatusOpen
	case CardStatusOpen, CardStatusClosed, CardStatusAll:
	default:
		return cardPageRequest{}, errors.New("status must be open, closed, or all")
	}
	if err := request.filters.validate(); err != nil {
		return cardPageRequest{}, err
	}
	request.size = input.PageSize
	switch {
	case request.size == 0:
		request.size = defaultCardPageSize
	case request.size < 1 || request.size > maximumCardPageSize:
		return cardPageRequest{}, errors.New("pageSize must be between 1 and 100")
	}
	request.query = url.Values{
		"filter": {string(status)}, "fields": {strings.Join(cardSummaryFields, ",")},
		"limit": {strconv.Itoa(request.size)}, "sort": {newestFirstSort},
	}
	if strings.TrimSpace(input.Before) != "" {
		before, err := validateTrelloID(input.Before, "before")
		if err != nil {
			return cardPageRequest{}, errors.New("before must be the nextBefore card ID from a previous page")
		}
		request.query.Set("before", before)
	}
	return request, nil
}

func (filters cardDueFilters) validate() error {
	hasRange := filters.dueBefore != nil || filters.dueAfter != nil
	switch {
	case hasRange && filters.hasDueDate != nil && !*filters.hasDueDate:
		return errors.New("dueBefore and dueAfter need cards with a due date, so hasDueDate cannot be false")
	case filters.dueBefore != nil && filters.dueAfter != nil && !filters.dueAfter.Before(*filters.dueBefore):
		return errors.New("dueAfter must be earlier than dueBefore")
	}
	return nil
}

// matches applies the due filters, which Trello's card list endpoints do not offer, to one card.
func (filters cardDueFilters) matches(card Card) bool {
	switch {
	case filters.hasDueDate != nil && *filters.hasDueDate != (card.Due != nil):
		return false
	case filters.isDueComplete != nil && *filters.isDueComplete != card.IsDueComplete:
		return false
	case filters.dueBefore != nil && (card.Due == nil || !card.Due.Before(*filters.dueBefore)):
		return false
	case filters.dueAfter != nil && (card.Due == nil || card.Due.Before(*filters.dueAfter)):
		return false
	}
	return true
}

func decodeCardPage(body []byte, request cardPageRequest) (ListCardsOutput, error) {
	resources, err := decodeJSONArray[cardResource](body)
	if err != nil {
		return ListCardsOutput{}, err
	}
	if len(resources) > request.size {
		return ListCardsOutput{}, errors.New("page holds more cards than requested")
	}
	output := ListCardsOutput{Cards: make([]Card, 0, len(resources)), ScannedCardCount: len(resources)}
	oldestCardID := ""
	for _, resource := range resources {
		card, err := decodeCard(resource, false)
		if err != nil {
			return ListCardsOutput{}, err
		}
		if oldestCardID == "" || strings.ToLower(card.ID) < strings.ToLower(oldestCardID) {
			oldestCardID = card.ID
		}
		if request.filters.matches(card) {
			output.Cards = append(output.Cards, card)
		}
	}
	if len(resources) == request.size {
		output.NextBefore = oldestCardID
	}
	return output, nil
}
