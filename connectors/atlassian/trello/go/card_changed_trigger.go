// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CardActionType is the Trello action type of one cardChanged delivery, as Trello names it.
type CardActionType string

const (
	// CardActionCreated is createCard: a card was added to a list on the board.
	CardActionCreated CardActionType = "createCard"
	// CardActionCopied is copyCard: a card was created as a copy of another card.
	CardActionCopied CardActionType = "copyCard"
	// CardActionConvertedFromCheckItem is convertToCardFromCheckItem: a checklist item became a card.
	CardActionConvertedFromCheckItem CardActionType = "convertToCardFromCheckItem"
	// CardActionEmailed is emailCard: a card was created by email to the board.
	CardActionEmailed CardActionType = "emailCard"
	// CardActionUpdated is updateCard: a field changed, such as the list (a move), name, description,
	// due date, position, or closed (archive or reopen). ChangedFields names the fields.
	CardActionUpdated CardActionType = "updateCard"
	// CardActionCommented is commentCard: a member commented on a card. CommentText holds the comment.
	CardActionCommented CardActionType = "commentCard"
	// CardActionMovedToBoard is moveCardToBoard: a card arrived from another board.
	CardActionMovedToBoard CardActionType = "moveCardToBoard"
	// CardActionDeleted is deleteCard: a card was deleted; Trello keeps only its ID, short ID, and list.
	CardActionDeleted CardActionType = "deleteCard"
)

// watchedCardActionTypes is the Trello action filter every cardChanged poll sends.
var watchedCardActionTypes = []CardActionType{
	CardActionCreated, CardActionCopied, CardActionConvertedFromCheckItem, CardActionEmailed,
	CardActionUpdated, CardActionCommented, CardActionMovedToBoard, CardActionDeleted,
}

// CardChangedTriggerConfiguration is the cardChanged binding configuration that the boardInput unit
// saves in Dex Web. Changing the board of a binding that already polled makes its next poll record a new
// baseline for the new board.
type CardChangedTriggerConfiguration struct {
	// BoardID is the watched board: its 24-character ID, such as 5b6893f01cb3228998cf629e, or its
	// 8-character short link from the board URL https://trello.com/b/<shortLink>/<name>. Required.
	BoardID string `json:"boardId"`
}

// Validate requires a board ID or short link without surrounding whitespace.
func (configuration CardChangedTriggerConfiguration) Validate() error {
	if !trelloIDPattern.MatchString(configuration.BoardID) && !shortLinkPattern.MatchString(configuration.BoardID) {
		return errors.New("cardChanged boardId must be a 24-character Trello board ID or an 8-character board short link")
	}
	return nil
}

// CardChangedEvent is one card action read from the watched board's action history.
type CardChangedEvent struct {
	// BoardID is the board the action belongs to, as Trello reports its 24-character ID.
	BoardID string `json:"boardId"`
	// BoardName is the board's name when the action recorded it.
	BoardName string `json:"boardName,omitempty"`
	// ActionID is Trello's 24-character action ID; it also identifies the Trigger event.
	ActionID string `json:"actionId"`
	// ActionType is the Trello action type; see CardActionType.
	ActionType CardActionType `json:"actionType"`
	// OccurredAt is when Trello recorded the action, in UTC with millisecond precision.
	OccurredAt time.Time `json:"occurredAt"`
	// MemberCreatorID is the ID of the member who performed the action.
	MemberCreatorID string `json:"memberCreatorId,omitempty"`
	// MemberCreator is that member's name and username when Trello returned them.
	MemberCreator *MemberReference `json:"memberCreator,omitempty"`
	// Card is the card as the action recorded it; read it with getCard for its current state.
	Card ActionCard `json:"card"`
	// List is the card's list when the action recorded one, such as for createCard.
	List *ActionList `json:"list,omitempty"`
	// ListBefore and ListAfter are set on an updateCard that moved the card between lists.
	ListBefore *ActionList `json:"listBefore,omitempty"`
	// ListAfter is the list the card moved to; see ListBefore.
	ListAfter *ActionList `json:"listAfter,omitempty"`
	// ChangedFields names the card fields an updateCard changed, such as idList, name, due, or closed,
	// sorted; empty for other action types.
	ChangedFields []string `json:"changedFields,omitempty"`
	// CommentText is the comment of a commentCard action.
	CommentText string `json:"commentText,omitempty"`
}

// ActionCard is the card snapshot a Trello action records.
type ActionCard struct {
	// ID is the card's 24-character ID.
	ID string `json:"id"`
	// Name is the card name when the action recorded it; empty for a deleted card.
	Name string `json:"name,omitempty"`
	// ShortLink is the card's 8-character short link, when recorded.
	ShortLink string `json:"shortLink,omitempty"`
	// ShortID is the card's number on its board, such as 42, when recorded.
	ShortID int64 `json:"shortId,omitempty"`
	// ListID is the card's list, when recorded.
	ListID string `json:"listId,omitempty"`
	// IsClosed reports the archived state an updateCard recorded; nil when the action did not record it.
	IsClosed *bool `json:"isClosed,omitempty"`
}

// ActionList is a list as a Trello action records it.
type ActionList struct {
	// ID is the list's 24-character ID.
	ID string `json:"id"`
	// Name is the list name when the action recorded it.
	Name string `json:"name,omitempty"`
}

// newCardChangedTriggerEvent wraps one action. Its ID is a pure function of the action ID, so a refetch
// after a crash yields the same Trigger event ID.
func newCardChangedTriggerEvent(event CardChangedEvent) sdkgo.TriggerEvent[CardChangedEvent] {
	return sdkgo.TriggerEvent[CardChangedEvent]{ID: cardChangeEventID(event.ActionID), OccurredAt: event.OccurredAt, Payload: event}
}

func cardChangeEventID(actionID string) string { return "trello-action-" + strings.ToLower(actionID) }

func cardChangedTriggerConfigurationError(err error) error {
	return fmt.Errorf("trello cardChanged configuration: %w", err)
}
