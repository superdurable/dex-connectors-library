// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package trello

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	updateCardOperationID    = "updateCard"
	updateCardFailureSubject = "card update"
)

// UpdateCardInput changes one card. Every set field is an absolute value, so sending the same update twice
// leaves the same card. Set at least one change; set at most one of LabelIDs and AddLabelIDs or
// RemoveLabelIDs, and at most one of Due and ShouldClearDue.
type UpdateCardInput struct {
	// CardID is the card's Trello ID.
	CardID string `json:"cardId"`
	// ListID moves the card to this list, which is how a Trello card changes status; blank leaves its list.
	ListID string `json:"listId,omitempty"`
	// BoardID names the board of ListID when that list is on another board; it requires ListID.
	BoardID string `json:"boardId,omitempty"`
	// Position is top, bottom, or a positive number in the card's list; blank leaves Trello's choice.
	Position string `json:"position,omitempty"`
	// IsClosed archives the card when true and reopens it when false; nil leaves it.
	IsClosed *bool `json:"isClosed,omitempty"`
	// Due sets when the card is due; nil leaves it.
	Due *time.Time `json:"due,omitempty"`
	// ShouldClearDue removes the card's due date.
	ShouldClearDue bool `json:"shouldClearDue,omitempty"`
	// IsDueComplete marks the due date complete when true and not complete when false; nil leaves it.
	IsDueComplete *bool `json:"isDueComplete,omitempty"`
	// LabelIDs replaces the card's labels with up to 50 label IDs of its board; a pointer to an empty list
	// removes every label, and nil leaves them.
	LabelIDs *[]string `json:"labelIds,omitempty"`
	// AddLabelIDs adds up to 50 label IDs to the card's current labels, read first.
	AddLabelIDs []string `json:"addLabelIds,omitempty"`
	// RemoveLabelIDs removes up to 50 label IDs from the card's current labels, read first.
	RemoveLabelIDs []string `json:"removeLabelIds,omitempty"`
	// MemberIDs replaces the card's members with up to 50 member IDs of its board; a pointer to an empty list
	// removes every member, and nil leaves them.
	MemberIDs *[]string `json:"memberIds,omitempty"`
}

// UpdateCardOutput reports the update. On updated, Card is the card read back after the change.
type UpdateCardOutput struct {
	// CardID echoes the requested card.
	CardID string `json:"cardId"`
	// Card is the card after the update, set only on updated.
	Card *Card `json:"card,omitempty"`
}

// UpdateCardOperation implements the updateCard Mutation.
type UpdateCardOperation struct{ client *Client }

// cardUpdate is the validated PUT body plus what the read-back must show.
type cardUpdate struct {
	cardID         string
	fields         map[string]any
	addLabelIDs    []string
	removeLabelIDs []string
	expectation    cardExpectation
}

// cardExpectation is every requested value a read-back card must carry for the update to count as applied.
type cardExpectation struct {
	listID        string
	boardID       string
	isClosed      *bool
	due           *time.Time
	isDueCleared  bool
	isDueComplete *bool
	labelIDs      []string
	hasLabelIDs   bool
	memberIDs     []string
	hasMemberIDs  bool
}

// Definition returns the immutable connector operation definition.
func (UpdateCardOperation) Definition() sdkgo.MutationDefinition { return UpdateCardDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Trello needs none: every
// change is an absolute value, so a repeated update converges on the same card.
func (UpdateCardOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCardInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads the card's labels when labels are added or removed, sends PUT /cards/{id} with the complete
// resulting values, and reads the card back to confirm them. Any ambiguous outcome is retried, because
// resending absolute values cannot change the card twice.
func (operation UpdateCardOperation) Invoke(call sdkgo.Call, input UpdateCardInput) sdkgo.MutationAttempt[UpdateCardOutput] {
	client := operation.client
	output := UpdateCardOutput{CardID: strings.TrimSpace(input.CardID)}
	update, err := buildCardUpdate(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateCardBranchDefect, output, failurePointer(updateCardOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, startFailure := client.startSession(call, updateCardOperationID)
	if startFailure != nil {
		return sdkgo.NewMutationBranch(UpdateCardBranchDefect, output, startFailure, sdkgo.Receipt{})
	}
	defer cancel()
	if len(update.addLabelIDs) > 0 || len(update.removeLabelIDs) > 0 {
		current, readReceipt, classification := client.readCard(session, updateCardOperationID, update.cardID)
		if classification.outcome != readSucceeded {
			return cardReadAttempt(classification, output, readReceipt)
		}
		if err := update.resolveLabelChanges(current); err != nil {
			return sdkgo.NewMutationBranch(UpdateCardBranchDefect, output, failurePointer(updateCardOperationID, sdkgo.FailureValidation, err.Error()), readReceipt)
		}
	}
	result := client.exchange(session, trelloRequest{method: http.MethodPut, path: joinPath("cards", update.cardID), payload: update.fields})
	if attempt, isFinal := cardWriteAttempt(client.classifyRepeatableWrite(updateCardOperationID, updateCardFailureSubject, result), output,
		client.receipt(session, result.response, update.cardID)); isFinal {
		return attempt
	}
	card, readReceipt, classification := client.readCard(session, updateCardOperationID, update.cardID)
	if classification.outcome != readSucceeded {
		return cardReadAttempt(classification, output, readReceipt)
	}
	if field := update.expectation.firstUnappliedField(card); field != "" {
		return sdkgo.NewMutationBranch(UpdateCardBranchInvalidResponse, output, failurePointer(updateCardOperationID, sdkgo.FailureProtocol,
			"Trello accepted the update but the card read back does not show the requested "+field), readReceipt)
	}
	output.Card = &card
	return sdkgo.NewMutationBranch(UpdateCardBranchUpdated, output, nil, readReceipt)
}

// cardWriteAttempt ends the operation for every outcome except an accepted write.
func cardWriteAttempt(classification writeClassification, output UpdateCardOutput, receipt sdkgo.Receipt) (sdkgo.MutationAttempt[UpdateCardOutput], bool) {
	switch classification.outcome {
	case writeAccepted:
		return sdkgo.MutationAttempt[UpdateCardOutput]{}, false
	case writeRetry:
		return sdkgo.NewMutationRetry[UpdateCardOutput](classification.failure, classification.retryAfter), true
	case writeNotFound:
		return sdkgo.NewMutationBranch(UpdateCardBranchNotFound, output, &classification.failure, receipt), true
	case writeRejected:
		return sdkgo.NewMutationBranch(UpdateCardBranchProviderRejected, output, &classification.failure, receipt), true
	case writeDefect:
		return sdkgo.NewMutationBranch(UpdateCardBranchDefect, output, &classification.failure, receipt), true
	default:
		return sdkgo.NewMutationBranch(UpdateCardBranchInvalidResponse, output, &classification.failure, receipt), true
	}
}

// cardReadAttempt maps a failed read before or after the PUT.
func cardReadAttempt(classification readClassification, output UpdateCardOutput, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdateCardOutput] {
	switch classification.outcome {
	case readRetry:
		return sdkgo.NewMutationRetry[UpdateCardOutput](classification.failure, classification.retryAfter)
	case readNotFound:
		return sdkgo.NewMutationBranch(UpdateCardBranchNotFound, output, &classification.failure, receipt)
	case readDefect:
		return sdkgo.NewMutationBranch(UpdateCardBranchDefect, output, &classification.failure, receipt)
	case readRejected:
		return sdkgo.NewMutationBranch(UpdateCardBranchProviderRejected, output, &classification.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(UpdateCardBranchInvalidResponse, output, &classification.failure, receipt)
	}
}

// buildCardUpdate validates input; fields holds Trello's PUT body with JSON null for a cleared due date.
func buildCardUpdate(input UpdateCardInput) (cardUpdate, error) {
	cardID, err := validateTrelloID(input.CardID, "cardId")
	if err != nil {
		return cardUpdate{}, err
	}
	update := cardUpdate{cardID: cardID, fields: map[string]any{}}
	if err := update.addPlacement(input); err != nil {
		return cardUpdate{}, err
	}
	if input.IsClosed != nil {
		update.fields["closed"] = *input.IsClosed
		update.expectation.isClosed = input.IsClosed
	}
	switch {
	case input.Due != nil && input.ShouldClearDue:
		return cardUpdate{}, errors.New("set at most one of due and shouldClearDue")
	case input.Due != nil:
		update.fields["due"] = formatTrelloTime(*input.Due)
		update.expectation.due = input.Due
	case input.ShouldClearDue:
		update.fields["due"] = nil
		update.expectation.isDueCleared = true
	}
	if input.IsDueComplete != nil {
		update.fields["dueComplete"] = *input.IsDueComplete
		update.expectation.isDueComplete = input.IsDueComplete
	}
	if err := update.addLabelChanges(input); err != nil {
		return cardUpdate{}, err
	}
	if input.MemberIDs != nil {
		memberIDs, err := validateTrelloIDs(*input.MemberIDs, "memberIds")
		if err != nil {
			return cardUpdate{}, err
		}
		update.fields["idMembers"] = strings.Join(memberIDs, ",")
		update.expectation.memberIDs, update.expectation.hasMemberIDs = memberIDs, true
	}
	if len(update.fields) == 0 && len(update.addLabelIDs) == 0 && len(update.removeLabelIDs) == 0 {
		return cardUpdate{}, errors.New("set at least one of listId, position, isClosed, due, shouldClearDue, isDueComplete, labelIds, addLabelIds, removeLabelIds, and memberIds")
	}
	return update, nil
}

func (update *cardUpdate) addPlacement(input UpdateCardInput) error {
	listID, err := validateOptionalTrelloID(input.ListID, "listId")
	if err != nil {
		return err
	}
	boardID, err := validateOptionalTrelloID(input.BoardID, "boardId")
	if err != nil {
		return err
	}
	if boardID != "" && listID == "" {
		return errors.New("boardId requires listId, the list on that board that receives the card")
	}
	if listID != "" {
		update.fields["idList"] = listID
		update.expectation.listID = listID
	}
	if boardID != "" {
		update.fields["idBoard"] = boardID
		update.expectation.boardID = boardID
	}
	position, err := cardPositionWireValue(input.Position)
	if err != nil {
		return err
	}
	if position != nil {
		update.fields["pos"] = position
	}
	return nil
}

func (update *cardUpdate) addLabelChanges(input UpdateCardInput) error {
	addLabelIDs, err := validateTrelloIDs(input.AddLabelIDs, "addLabelIds")
	if err != nil {
		return err
	}
	removeLabelIDs, err := validateTrelloIDs(input.RemoveLabelIDs, "removeLabelIds")
	if err != nil {
		return err
	}
	for _, labelID := range addLabelIDs {
		if containsTrelloID(removeLabelIDs, labelID) {
			return fmt.Errorf("label %s is both added and removed", labelID)
		}
	}
	isChangingLabels := len(addLabelIDs) > 0 || len(removeLabelIDs) > 0
	switch {
	case input.LabelIDs != nil && isChangingLabels:
		return errors.New("set either labelIds or addLabelIds and removeLabelIds, not both")
	case input.LabelIDs != nil:
		labelIDs, err := validateTrelloIDs(*input.LabelIDs, "labelIds")
		if err != nil {
			return err
		}
		update.fields["idLabels"] = strings.Join(labelIDs, ",")
		update.expectation.labelIDs, update.expectation.hasLabelIDs = labelIDs, true
	}
	update.addLabelIDs, update.removeLabelIDs = addLabelIDs, removeLabelIDs
	return nil
}

// resolveLabelChanges turns added and removed labels into the complete label set the PUT sends.
func (update *cardUpdate) resolveLabelChanges(current Card) error {
	if current.HasMoreLabels {
		return fmt.Errorf("the card carries more than %d labels, so set labelIds with the complete list instead", maximumListedLabels)
	}
	labelIDs := make([]string, 0, len(current.LabelIDs)+len(update.addLabelIDs))
	for _, labelID := range current.LabelIDs {
		if !containsTrelloID(update.removeLabelIDs, labelID) {
			labelIDs = append(labelIDs, labelID)
		}
	}
	for _, labelID := range update.addLabelIDs {
		if !containsTrelloID(labelIDs, labelID) {
			labelIDs = append(labelIDs, labelID)
		}
	}
	if len(labelIDs) > maximumListedLabels {
		return fmt.Errorf("the card would carry more than %d labels", maximumListedLabels)
	}
	update.fields["idLabels"] = strings.Join(labelIDs, ",")
	update.expectation.labelIDs, update.expectation.hasLabelIDs = labelIDs, true
	return nil
}

// firstUnappliedField names the first requested value the read-back card does not carry, or "".
func (expectation cardExpectation) firstUnappliedField(card Card) string {
	switch {
	case expectation.listID != "" && !strings.EqualFold(card.ListID, expectation.listID):
		return "list"
	case expectation.boardID != "" && !strings.EqualFold(card.BoardID, expectation.boardID):
		return "board"
	case expectation.isClosed != nil && card.IsClosed != *expectation.isClosed:
		return "archive state"
	case expectation.due != nil && (card.Due == nil || !isSameTrelloInstant(*card.Due, *expectation.due)):
		return "due date"
	case expectation.isDueCleared && card.Due != nil:
		return "cleared due date"
	case expectation.isDueComplete != nil && card.IsDueComplete != *expectation.isDueComplete:
		return "due-complete state"
	case expectation.hasLabelIDs && (card.HasMoreLabels || !isSameTrelloIDSet(card.LabelIDs, expectation.labelIDs)):
		return "labels"
	case expectation.hasMemberIDs && !isSameTrelloIDSet(card.MemberIDs, expectation.memberIDs):
		return "members"
	}
	return ""
}

func containsTrelloID(values []string, wanted string) bool {
	for _, value := range values {
		if strings.EqualFold(value, wanted) {
			return true
		}
	}
	return false
}

// isSameTrelloIDSet compares two duplicate-free ID lists without regard to order or letter case.
func isSameTrelloIDSet(first []string, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for _, value := range first {
		if !containsTrelloID(second, value) {
			return false
		}
	}
	return true
}
